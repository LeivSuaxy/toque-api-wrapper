package ratelimit

import (
	"math"
	"sync"
	"time"
)

type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens     float64
	last       time.Time
	ratePerSec float64
	capacity   float64
}

type Result struct {
	Allowed   bool
	Limit     int
	Remaining int
	Reset     time.Time
}

func New() *Limiter {
	return &Limiter{
		buckets: make(map[string]*bucket),
		now:     time.Now,
	}
}

func (l *Limiter) Allow(key string, limitPerMinute, burst int) Result {
	if limitPerMinute <= 0 {
		return Result{Allowed: true, Limit: 0, Remaining: 0, Reset: l.now()}
	}

	now := l.now()
	ratePerSec := float64(limitPerMinute) / 60.0
	if burst <= 0 {
		burst = limitPerMinute
	}
	if burst < 1 {
		burst = 1
	}
	capacity := float64(burst)

	l.mu.Lock()
	defer l.mu.Unlock()

	current, ok := l.buckets[key]
	if !ok {
		current = &bucket{
			tokens:     capacity,
			last:       now,
			ratePerSec: ratePerSec,
			capacity:   capacity,
		}
		l.buckets[key] = current
	} else {
		elapsed := now.Sub(current.last).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		current.tokens = math.Min(capacity, current.tokens+elapsed*current.ratePerSec)
		current.last = now
		current.ratePerSec = ratePerSec
		current.capacity = capacity
	}

	if current.tokens >= 1 {
		current.tokens--
		return Result{
			Allowed:   true,
			Limit:     limitPerMinute,
			Remaining: int(math.Floor(current.tokens)),
			Reset:     resetAt(now, current),
		}
	}

	return Result{
		Allowed:   false,
		Limit:     limitPerMinute,
		Remaining: 0,
		Reset:     resetAt(now, current),
	}
}

func (l *Limiter) Cleanup(maxIdle time.Duration) {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	for key, current := range l.buckets {
		if now.Sub(current.last) > maxIdle {
			delete(l.buckets, key)
		}
	}
}

func resetAt(now time.Time, current *bucket) time.Time {
	if current.ratePerSec <= 0 {
		return now
	}
	missing := current.capacity - current.tokens
	if missing <= 0 {
		return now
	}
	return now.Add(time.Duration(missing / current.ratePerSec * float64(time.Second)))
}
