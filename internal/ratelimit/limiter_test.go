package ratelimit

import (
	"testing"
	"time"
)

func TestAllowAndRefill(t *testing.T) {
	limiter := New()
	current := time.Unix(0, 0)
	limiter.now = func() time.Time { return current }

	first := limiter.Allow("key", 60, 2)
	if !first.Allowed || first.Remaining != 1 {
		t.Fatalf("first = %+v", first)
	}

	second := limiter.Allow("key", 60, 2)
	if !second.Allowed || second.Remaining != 0 {
		t.Fatalf("second = %+v", second)
	}

	third := limiter.Allow("key", 60, 2)
	if third.Allowed {
		t.Fatalf("third should be denied: %+v", third)
	}

	current = current.Add(time.Second)
	fourth := limiter.Allow("key", 60, 2)
	if !fourth.Allowed {
		t.Fatalf("fourth should be allowed after refill: %+v", fourth)
	}
}

func TestUnlimited(t *testing.T) {
	limiter := New()
	for i := 0; i < 100; i++ {
		result := limiter.Allow("key", 0, 0)
		if !result.Allowed {
			t.Fatalf("unlimited limiter denied request")
		}
	}
}

func TestPerKeyIsolation(t *testing.T) {
	limiter := New()
	current := time.Unix(0, 0)
	limiter.now = func() time.Time { return current }

	if result := limiter.Allow("a", 60, 1); !result.Allowed {
		t.Fatalf("a first denied")
	}
	if result := limiter.Allow("a", 60, 1); result.Allowed {
		t.Fatalf("a second should be denied")
	}
	if result := limiter.Allow("b", 60, 1); !result.Allowed {
		t.Fatalf("b should be independent")
	}
}

func TestCleanup(t *testing.T) {
	limiter := New()
	current := time.Unix(0, 0)
	limiter.now = func() time.Time { return current }

	limiter.Allow("old", 60, 1)

	current = current.Add(2 * time.Hour)
	limiter.Cleanup(time.Hour)

	if len(limiter.buckets) != 0 {
		t.Fatalf("buckets not cleaned: %d", len(limiter.buckets))
	}
}
