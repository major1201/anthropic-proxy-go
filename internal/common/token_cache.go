// Package common provides an in-memory token count cache.
package common

import "sync"

var (
	cache   = make(map[string]int)
	cacheMu sync.RWMutex
)

// CacheTokens stores a token count for a request ID.
func CacheTokens(requestID string, tokens int) {
	if requestID == "" || tokens <= 0 {
		return
	}
	cacheMu.Lock()
	cache[requestID] = tokens
	cacheMu.Unlock()
}

// GetCachedTokens retrieves and optionally removes the cached token count.
func GetCachedTokens(requestID string, remove bool) (int, bool) {
	if requestID == "" {
		return 0, false
	}
	cacheMu.RLock()
	tokens, ok := cache[requestID]
	cacheMu.RUnlock()

	if remove && ok {
		cacheMu.Lock()
		delete(cache, requestID)
		cacheMu.Unlock()
	}
	return tokens, ok
}

// ClearCache clears all cached entries.
func ClearCache() {
	cacheMu.Lock()
	cache = make(map[string]int)
	cacheMu.Unlock()
}

// CacheSize returns the current number of cached entries.
func CacheSize() int {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return len(cache)
}
