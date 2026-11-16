package people

import (
	"sync"
	"time"
)

const maxBackoff = 15 * time.Minute

// Limiter 按登录名和 IP 做指数退避. 不锁账号.
type Limiter struct {
	mu    sync.Mutex
	fails map[string]int
	until map[string]time.Time
}

// NewLimiter 返回空的限速器.
func NewLimiter() *Limiter {
	return &Limiter{fails: map[string]int{}, until: map[string]time.Time{}}
}

// Rejected 返回还要等待多久. 0 表示可以尝试.
func (l *Limiter) Rejected(key string, now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	until := l.until[key]
	if now.Before(until) {
		return until.Sub(now)
	}
	return 0
}

// Fail 记录一次失败并拉长等待.
func (l *Limiter) Fail(key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[key]++
	wait := time.Second
	for n := 1; n < l.fails[key] && wait < maxBackoff; n++ {
		wait *= 2
	}
	if wait > maxBackoff {
		wait = maxBackoff
	}
	l.until[key] = now.Add(wait)
}

// Success 清除这个键的失败记录.
func (l *Limiter) Success(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, key)
	delete(l.until, key)
}
