package middleware

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

// RateLimiter 速率限制器
type RateLimiter struct {
	enabled   bool
	limiters  map[string]*rate.Limiter
	mu        sync.RWMutex
	r         rate.Limit
	b         int
	maxConns  int
	connCount int32
	connMu    sync.Mutex
}

// NewRateLimiter 创建新的速率限制器
func NewRateLimiter(enabled bool, requestsPerSecond int, burstSize int, maxConnections int) *RateLimiter {
	return &RateLimiter{
		enabled:  enabled,
		limiters: make(map[string]*rate.Limiter),
		r:        rate.Limit(requestsPerSecond),
		b:        burstSize,
		maxConns: maxConnections,
	}
}

// getLimiter 获取或创建IP的限制器
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rl.r, rl.b)
		rl.limiters[ip] = limiter
	}

	return limiter
}

// cleanupLimiters 清理过期的限制器
func (rl *RateLimiter) cleanupLimiters() {
	ticker := time.NewTicker(time.Minute)
	go func() {
		for range ticker.C {
			rl.mu.Lock()
			for ip, limiter := range rl.limiters {
				if limiter.Allow() {
					// 如果限制器允许请求，说明可能长时间未使用，删除它
					delete(rl.limiters, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
}

// Middleware 速率限制中间件
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	// 如果限速器未启用，直接跳过
	if !rl.enabled {
		return next
	}

	// 启动清理协程
	rl.cleanupLimiters()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 检查连接数限制
		rl.connMu.Lock()
		if rl.connCount >= int32(rl.maxConns) {
			rl.connMu.Unlock()
			http.Error(w, "Too many connections", http.StatusTooManyRequests)
			return
		}
		rl.connCount++
		rl.connMu.Unlock()

		// 连接结束时减少计数
		defer func() {
			rl.connMu.Lock()
			rl.connCount--
			rl.connMu.Unlock()
		}()

		// 获取客户端IP
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = forwarded
		}

		// 检查速率限制
		limiter := rl.getLimiter(ip)
		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// GetStats 获取统计信息
func (rl *RateLimiter) GetStats() map[string]interface{} {
	// 使用原子操作获取连接数
	currentConns := atomic.LoadInt32(&rl.connCount)

	// 只对limiters map使用读锁
	rl.mu.RLock()
	activeLimiters := len(rl.limiters)
	rl.mu.RUnlock()

	return map[string]interface{}{
		"enabled":             rl.enabled,
		"active_limiters":     activeLimiters,
		"current_connections": currentConns,
		"max_connections":     rl.maxConns,
		"requests_per_second": float64(rl.r),
		"burst_size":          rl.b,
	}
}
