// Command proxy starts the Anthropic-OpenAI proxy server.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/major1201/anthropic-proxy-go/internal/common"
	"github.com/major1201/anthropic-proxy-go/internal/config"
	"github.com/major1201/anthropic-proxy-go/internal/handler"
	appmiddleware "github.com/major1201/anthropic-proxy-go/internal/middleware"
)

func main() {
	configPath := flag.String("config", "", "JSON config file path (default: config/settings.json)")
	flag.Parse()

	// Determine config path
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = config.GetConfigFilePath()
	}

	// Load config
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	config.SetConfig(cfg)

	// Configure logging
	common.ConfigureLogging(cfg.Logging.Level)

	// Print startup info
	addr := cfg.GetServerAddr()
	fmt.Printf("Starting Anthropic-OpenAI Proxy (Go)...\n")
	fmt.Printf("   Config file: %s\n", cfgPath)
	fmt.Printf("   Listening on: %s\n", addr)
	fmt.Println()
	fmt.Println("Key endpoints:")
	fmt.Printf("   Health check: http://%s/health\n", addr)
	fmt.Printf("   API: http://%s/v1/messages\n", addr)
	fmt.Println()

	// Create messages handler
	msgHandler := handler.NewMessagesHandler(cfg)

	// Create router
	r := chi.NewRouter()

	// Global middleware
	r.Use(chimiddleware.Recoverer)
	r.Use(appmiddleware.RequestTimingMiddleware())

	// CORS
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	}))

	// API key middleware
	r.Use(appmiddleware.APIKeyMiddleware(cfg.APIKey))

	// Routes
	r.Get("/", handler.RootHandler)
	r.Get("/health", handler.HealthHandler)
	r.Post("/v1/messages", msgHandler.ServeHTTP)

	// Start config watcher
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	watcher := config.NewConfigWatcher(cfgPath, func(newCfg *config.Config) error {
		config.SetConfig(newCfg)
		common.ConfigureLogging(newCfg.Logging.Level)
		// Note: msgHandler is recreated with new config reference
		// The handler reads config from the global singleton, so it picks up changes automatically
		return nil
	})
	go watcher.Start(ctx)

	// Start server
	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // Long timeout for streaming
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\nShutting down...")
		watcher.Stop()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}

	fmt.Println("Server stopped")
}
