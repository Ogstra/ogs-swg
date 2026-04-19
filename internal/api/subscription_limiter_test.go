package api

import (
	"testing"
	"time"
)

func TestSubscriptionLimiter(t *testing.T) {
	t.Run("allows below max requests", func(t *testing.T) {
		limiter := newSubscriptionLimiter()
		limiter.record("token-a")

		blocked, retryAfter := limiter.check("token-a", 2, time.Minute)
		if blocked {
			t.Fatalf("expected request to be allowed")
		}
		if retryAfter != 0 {
			t.Fatalf("expected zero retryAfter, got %v", retryAfter)
		}
	})

	t.Run("blocks at max requests", func(t *testing.T) {
		limiter := newSubscriptionLimiter()
		limiter.record("token-a")
		limiter.record("token-a")

		blocked, retryAfter := limiter.check("token-a", 2, time.Minute)
		if !blocked {
			t.Fatalf("expected request to be blocked")
		}
		if retryAfter <= 0 {
			t.Fatalf("expected positive retryAfter, got %v", retryAfter)
		}
	})

	t.Run("prunes expired timestamps", func(t *testing.T) {
		limiter := newSubscriptionLimiter()
		limiter.requests["token-a"] = []time.Time{time.Now().Add(-2 * time.Minute)}

		blocked, retryAfter := limiter.check("token-a", 1, time.Minute)
		if blocked {
			t.Fatalf("expected expired request to be pruned")
		}
		if retryAfter != 0 {
			t.Fatalf("expected zero retryAfter, got %v", retryAfter)
		}
		if _, ok := limiter.requests["token-a"]; ok {
			t.Fatalf("expected empty token entry to be removed")
		}
	})

	t.Run("isolates tokens", func(t *testing.T) {
		limiter := newSubscriptionLimiter()
		limiter.record("token-a")
		limiter.record("token-b")
		limiter.record("token-b")

		blockedA, _ := limiter.check("token-a", 2, time.Minute)
		blockedB, _ := limiter.check("token-b", 2, time.Minute)
		if blockedA {
			t.Fatalf("expected token-a to remain allowed")
		}
		if !blockedB {
			t.Fatalf("expected token-b to be blocked")
		}
	})

	t.Run("retryAfter uses oldest timestamp in window", func(t *testing.T) {
		limiter := newSubscriptionLimiter()
		oldest := time.Now().Add(-40 * time.Second)
		limiter.requests["token-a"] = []time.Time{oldest, time.Now().Add(-10 * time.Second)}

		blocked, retryAfter := limiter.check("token-a", 2, time.Minute)
		if !blocked {
			t.Fatalf("expected request to be blocked")
		}

		expected := oldest.Add(time.Minute).Sub(time.Now())
		if retryAfter < expected-2*time.Second || retryAfter > expected+2*time.Second {
			t.Fatalf("expected retryAfter around %v, got %v", expected, retryAfter)
		}
	})
}
