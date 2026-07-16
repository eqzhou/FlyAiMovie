package httpapi

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	maxTrackedRateLimitSources = 10_000
	overflowRateLimitSource    = "__overflow__"
)

type rateLimitEntry struct {
	count   int
	resetAt time.Time
}

type ipRateLimiter struct {
	mu          sync.Mutex
	limit       int
	window      time.Duration
	entries     map[string]rateLimitEntry
	now         func() time.Time
	lastCleanup time.Time
}

func newIPRateLimiter(limit int, window time.Duration, now func() time.Time) *ipRateLimiter {
	if limit <= 0 {
		limit = 600
	}
	if window <= 0 {
		window = time.Minute
	}
	if now == nil {
		now = time.Now
	}
	return &ipRateLimiter{
		limit:       limit,
		window:      window,
		entries:     make(map[string]rateLimitEntry),
		now:         now,
		lastCleanup: now(),
	}
}

func (l *ipRateLimiter) Allow(key string) (bool, time.Duration) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastCleanup) >= l.window {
		for entryKey, entry := range l.entries {
			if !entry.resetAt.After(now) {
				delete(l.entries, entryKey)
			}
		}
		l.lastCleanup = now
	}

	entry, exists := l.entries[key]
	if !exists && len(l.entries) >= maxTrackedRateLimitSources {
		key = overflowRateLimitSource
		entry, exists = l.entries[key]
	}
	if !exists || !entry.resetAt.After(now) {
		l.entries[key] = rateLimitEntry{count: 1, resetAt: now.Add(l.window)}
		return true, l.window
	}
	if entry.count >= l.limit {
		return false, entry.resetAt.Sub(now)
	}
	entry.count++
	l.entries[key] = entry
	return true, entry.resetAt.Sub(now)
}

func (s *Server) rateLimit(limiter *ipRateLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}

		allowed, retryAfter := limiter.Allow(remoteIP(c.Request.RemoteAddr))
		remainingSeconds := int64((retryAfter + time.Second - 1) / time.Second)
		c.Header("X-RateLimit-Limit", strconv.Itoa(limiter.limit))
		c.Header("X-RateLimit-Reset", strconv.FormatInt(limiter.now().Add(retryAfter).Unix(), 10))
		if !allowed {
			c.Header("Retry-After", strconv.FormatInt(remainingSeconds, 10))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code":    http.StatusTooManyRequests,
				"message": "too many requests",
			})
			return
		}
		c.Next()
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	if ip := net.ParseIP(remoteAddr); ip != nil {
		return ip.String()
	}
	return "unknown"
}
