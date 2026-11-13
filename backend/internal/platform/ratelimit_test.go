package platform

import (
	"context"
	"testing"
	"time"
)

type limiterClock struct{ now time.Time }

func (c *limiterClock) Now() time.Time { return c.now }

func TestRateLimitBurstRefillsAndSeparatesSubjects(t *testing.T) {
	clock := &limiterClock{time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)}
	lim := NewRateLimiter(nil, clock)
	for range 10 {
		if retry, err := lim.Check(context.Background(), "login:one", 10, time.Minute); err != nil || retry != 0 {
			t.Fatalf("early rejection %v %v", retry, err)
		}
	}
	if retry, err := lim.Check(context.Background(), "login:one", 10, time.Minute); err != nil || retry != 6*time.Second {
		t.Fatalf("retry %v %v", retry, err)
	}
	if retry, err := lim.Check(context.Background(), "login:two", 10, time.Minute); err != nil || retry != 0 {
		t.Fatal("subjects shared quota")
	}
	clock.now = clock.now.Add(6 * time.Second)
	if retry, err := lim.Check(context.Background(), "login:one", 10, time.Minute); err != nil || retry != 0 {
		t.Fatal("quota did not refill")
	}
}

func TestRateLimitRejectsInvalidPolicyAndCancelledRequests(t *testing.T) {
	lim := NewRateLimiter(nil, RealClock{})
	for _, n := range []int{0, -1} {
		if _, err := lim.Check(context.Background(), "key", n, time.Minute); err == nil {
			t.Fatal("invalid quota accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := lim.Check(ctx, "key", 1, time.Second); err == nil {
		t.Fatal("cancelled request admitted")
	}
}
