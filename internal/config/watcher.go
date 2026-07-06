// Package config provides config file watching and hot reload.
package config

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ReloadCallback is called when the config file changes.
type ReloadCallback func(*Config) error

// ConfigWatcher watches a config file for changes and triggers reloads.
type ConfigWatcher struct {
	path     string
	watcher  *fsnotify.Watcher
	onReload ReloadCallback
	done     chan struct{}
}

// NewConfigWatcher creates a new ConfigWatcher.
func NewConfigWatcher(path string, onReload ReloadCallback) *ConfigWatcher {
	return &ConfigWatcher{
		path:     path,
		onReload: onReload,
		done:     make(chan struct{}),
	}
}

// Start begins watching the config file for changes.
func (w *ConfigWatcher) Start(ctx context.Context) error {
	var err error
	w.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	// Watch the config file's directory
	dir := "."
	if w.path != "" {
		dir = w.path
		// Extract directory from path
		for i := len(dir) - 1; i >= 0; i-- {
			if dir[i] == '/' {
				dir = dir[:i]
				break
			}
		}
	}

	if err := w.watcher.Add(dir); err != nil {
		return err
	}

	slog.Info("started watching config file", "path", w.path)

	go func() {
		defer w.watcher.Close()

		// Debounce timer
		var debounceTimer *time.Timer
		const debounceDelay = 200 * time.Millisecond

		for {
			select {
			case <-ctx.Done():
				return
			case <-w.done:
				return
			case event, ok := <-w.watcher.Events:
				if !ok {
					return
				}

				// Only react to write and create events on the config file
				if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
					continue
				}

				// Check if the event is for our config file
				if event.Name != w.path && !isSameFile(event.Name, w.path) {
					continue
				}

				// Debounce
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(debounceDelay, func() {
					w.reload()
				})

			case err, ok := <-w.watcher.Errors:
				if !ok {
					return
				}
				slog.Error("config watcher error", "error", err)
			}
		}
	}()

	return nil
}

// Stop stops the config watcher.
func (w *ConfigWatcher) Stop() {
	close(w.done)
	if w.watcher != nil {
		w.watcher.Close()
	}
}

func (w *ConfigWatcher) reload() {
	slog.Info("config file change detected, reloading")

	// Validate JSON first
	data, err := os.ReadFile(w.path)
	if err != nil {
		slog.Error("failed to read config file", "error", err)
		return
	}

	var testJSON interface{}
	if err := json.Unmarshal(data, &testJSON); err != nil {
		slog.Error("config file has invalid JSON, skipping reload", "error", err)
		return
	}

	// Load new config
	newCfg, err := LoadConfig(w.path)
	if err != nil {
		slog.Error("failed to reload config", "error", err)
		return
	}

	// Call reload callback
	if w.onReload != nil {
		if err := w.onReload(newCfg); err != nil {
			slog.Error("config reload callback failed", "error", err)
			return
		}
	}

	slog.Info("config reloaded successfully")
}

func isSameFile(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(infoA, infoB)
}
