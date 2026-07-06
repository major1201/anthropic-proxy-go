// Package middleware provides request timing and logging middleware.
package middleware

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/major1201/anthropic-proxy-go/internal/common"
)

// RequestTimingMiddleware logs request timing and adds request ID headers.
func RequestTimingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			startTime := time.Now()

			requestID := common.GenerateRequestID()
			ctx := common.WithRequestID(r.Context(), requestID)
			r = r.WithContext(ctx)

			logger := common.LoggerWithRequestID(ctx)

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("request processing panic",
						"error", rec,
						"url", r.URL.String(),
						"method", r.Method,
					)

					wrapped.Header().Set("X-Request-ID", requestID)
					wrapped.Header().Set("Content-Type", "application/json")
					wrapped.WriteHeader(http.StatusInternalServerError)
					json.NewEncoder(wrapped).Encode(map[string]interface{}{
						"error":      "Internal Server Error",
						"request_id": requestID,
					})
				}
			}()

			next.ServeHTTP(wrapped, r)

			responseTime := time.Since(startTime)
			responseTimeMS := float64(responseTime.Microseconds()) / 1000.0

			logger.Info("request completed",
				"status", wrapped.statusCode,
				"duration_ms", responseTimeMS,
			)

			wrapped.Header().Set("X-Process-Time", formatDuration(responseTime))
			wrapped.Header().Set("X-Request-ID", requestID)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for flusher support.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

func formatDuration(d time.Duration) string {
	return d.String()
}
