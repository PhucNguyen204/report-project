//go:build integration

package platform

import (
	"context"
	"github.com/redis/go-redis/v9"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRedisRateLimitSharesAtomicQuotaAcrossInstances(t *testing.T) {
	url := os.Getenv("TEST_REDIS_URL")
	if url == "" {
		url = "redis://127.0.0.1:56379/0"
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, second := redis.NewClient(opts), redis.NewClient(opts)
	t.Cleanup(func() { first.Close(); second.Close() })
	if err := first.Ping(ctx).Err(); err != nil {
		t.Fatalf("real Redis required: %v", err)
	}
	clock := &limiterClock{time.Now()}
	limiters := []*RateLimiter{NewRateLimiter(first, clock), NewRateLimiter(second, clock)}
	key := "atomic-test:" + ID()
	t.Cleanup(func() { first.Del(ctx, "dicho:rl:"+key) })
	var allowed atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			retry, err := limiters[i%2].Check(ctx, key, 10, time.Minute)
			if err != nil {
				t.Error(err)
			} else if retry == 0 {
				allowed.Add(1)
			} else if retry != 6*time.Second {
				t.Errorf("retry=%s", retry)
			}
		}()
	}
	close(start)
	wg.Wait()
	if allowed.Load() != 10 || limiters[0].Degraded() || limiters[1].Degraded() {
		t.Fatalf("shared quota allowed=%d degraded=%v/%v", allowed.Load(), limiters[0].Degraded(), limiters[1].Degraded())
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if retry, err := limiters[0].Check(ctx, "fallback:"+key, 1, time.Minute); err != nil || retry != 0 || !limiters[0].Degraded() {
		t.Fatalf("outage fallback retry=%s err=%v degraded=%v", retry, err, limiters[0].Degraded())
	}
	if retry, err := limiters[0].Check(ctx, "fallback:"+key, 1, time.Minute); err != nil || retry != time.Minute {
		t.Fatalf("fallback quota retry=%s err=%v", retry, err)
	}
}
