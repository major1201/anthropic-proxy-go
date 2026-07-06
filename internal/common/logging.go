// Package common provides shared utilities for logging, token caching, and counting.
package common

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/google/uuid"
)

type contextKey string

const requestIDKey contextKey = "request_id"

// GenerateRequestID generates a unique request ID in the format req_<uuid>.
func GenerateRequestID() string {
	return "req_" + strings.ReplaceAll(uuid.New().String(), "-", "")
}

// WithRequestID stores a request ID in the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// GetRequestID extracts the request ID from context.
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// ConfigureLogging sets up the slog logger with the specified level.
func ConfigureLogging(level string) {
	var slogLevel slog.Level
	switch strings.ToUpper(level) {
	case "DEBUG":
		slogLevel = slog.LevelDebug
	case "INFO":
		slogLevel = slog.LevelInfo
	case "WARN", "WARNING":
		slogLevel = slog.LevelWarn
	case "ERROR":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}

	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slogLevel,
	})
	slog.SetDefault(slog.New(handler))
}

// LoggerWithRequestID returns a logger with the request ID from context.
func LoggerWithRequestID(ctx context.Context) *slog.Logger {
	requestID := GetRequestID(ctx)
	if requestID == "" {
		requestID = "---"
	}
	return slog.With("request_id", requestID)
}

// GetRequestIDFromRequest is a helper that returns the request ID or a default.
func GetRequestIDFromRequest(requestID string) string {
	if requestID == "" {
		return "---"
	}
	return requestID
}

// SafeLogger returns a logger with the given request ID.
func SafeLogger(requestID string) *slog.Logger {
	if requestID == "" {
		requestID = "---"
	}
	return slog.With("request_id", requestID)
}

// LogException logs an exception with context.
func LogException(logger *slog.Logger, message string, err error) {
	logger.Error(message, "error", fmt.Sprintf("%v", err))
}
