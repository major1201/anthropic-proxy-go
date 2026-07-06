// Package middleware provides HTTP middleware for the proxy.
package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/major1201/anthropic-proxy-go/internal/models"
)

// authRequiredPaths lists paths that require API key validation.
var authRequiredPaths = map[string]bool{
	"/v1/messages": true,
}

// APIKeyMiddleware returns a middleware that validates API keys.
func APIKeyMiddleware(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !authRequiredPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Check x-api-key header
			token := r.Header.Get("x-api-key")

			// Check Authorization: Bearer header
			if token != apiKey {
				authHeader := r.Header.Get("Authorization")
				if strings.HasPrefix(authHeader, "Bearer ") {
					bearerToken := strings.TrimPrefix(authHeader, "Bearer ")
					if bearerToken == apiKey {
						next.ServeHTTP(w, r)
						return
					}
				}
			}

			if token == apiKey {
				next.ServeHTTP(w, r)
				return
			}

			// Return 401
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			errorResp := models.GetErrorResponse(401, "Invalid API key", nil)
			json.NewEncoder(w).Encode(errorResp)
		})
	}
}
