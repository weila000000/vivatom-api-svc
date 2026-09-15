package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const requestIDHeader = "X-Request-ID"

func requestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(requestIDHeader)
		if !validRequestID(id) {
			var value [16]byte
			if _, err := rand.Read(value[:]); err == nil {
				id = hex.EncodeToString(value[:])
			} else {
				id = time.Now().UTC().Format("20060102150405.000000000")
			}
		}
		c.Set("request_id", id)
		c.Header(requestIDHeader, id)
		c.Next()
	}
}

func validRequestID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' && char != '.' {
			return false
		}
	}
	return true
}

func accessLog(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http_request",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(started).Milliseconds(),
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		)
	}
}

type rateBucket struct {
	started time.Time
	count   int
}

type rateLimiter struct {
	mu          sync.Mutex
	buckets     map[string]rateBucket
	limit       int
	window      time.Duration
	now         func() time.Time
	lastCleanup time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{buckets: make(map[string]rateBucket), limit: limit, window: window, now: time.Now}
}

func (l *rateLimiter) middleware(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		now := l.now()
		key := scope + ":" + c.ClientIP()
		l.mu.Lock()
		if l.lastCleanup.IsZero() || now.Sub(l.lastCleanup) >= l.window {
			for bucketKey, candidate := range l.buckets {
				if now.Sub(candidate.started) >= l.window {
					delete(l.buckets, bucketKey)
				}
			}
			l.lastCleanup = now
		}
		bucket := l.buckets[key]
		if bucket.started.IsZero() || now.Sub(bucket.started) >= l.window {
			bucket = rateBucket{started: now}
		}
		bucket.count++
		l.buckets[key] = bucket
		remaining := max(0, l.limit-bucket.count)
		reset := max(1, int(bucket.started.Add(l.window).Sub(now).Seconds()))
		l.mu.Unlock()

		c.Header("X-RateLimit-Limit", intString(l.limit))
		c.Header("X-RateLimit-Remaining", intString(remaining))
		c.Header("X-RateLimit-Reset", intString(reset))
		if bucket.count > l.limit {
			c.Header("Retry-After", intString(reset))
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": gin.H{"code": "rate_limited", "message": "请求过于频繁，请稍后重试"}})
			return
		}
		c.Next()
	}
}

func intString(value int) string {
	return strconv.Itoa(value)
}
