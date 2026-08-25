package handlers

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimiter struct {
	mu      sync.Mutex
	entries map[string]*rlEntry
	rate    int
	window  time.Duration
}

type rlEntry struct {
	count     int
	windowEnd time.Time
}

func newRateLimiter(ctx context.Context, rate int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{entries: map[string]*rlEntry{}, rate: rate, window: window}
	ticker := time.NewTicker(time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rl.mu.Lock()
				now := time.Now()
				for k, e := range rl.entries {
					if now.After(e.windowEnd) {
						delete(rl.entries, k)
					}
				}
				rl.mu.Unlock()
			case <-ctx.Done():
				return
			}
		}
	}()
	return rl
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	e, ok := rl.entries[key]
	if !ok || now.After(e.windowEnd) {
		rl.entries[key] = &rlEntry{count: 1, windowEnd: now.Add(rl.window)}
		return true
	}
	if e.count >= rl.rate {
		return false
	}
	e.count++
	return true
}

func (rl *rateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rl.allow(c.ClientIP()) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"data":  nil,
				"error": "too many requests, try again later",
			})
			return
		}
		c.Next()
	}
}
