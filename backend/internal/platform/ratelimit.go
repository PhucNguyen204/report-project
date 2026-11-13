package platform

import (
	"context"
	"errors"
	"github.com/redis/go-redis/v9"
	"sync"
	"sync/atomic"
	"time"
)

type RateLimiter struct {
	client   *redis.Client
	clock    Clock
	mu       sync.Mutex
	tat      map[string]int64
	degraded atomic.Bool
}

func NewRateLimiter(client *redis.Client, clock Clock) *RateLimiter {
	if clock == nil {
		clock = RealClock{}
	}
	r := &RateLimiter{client: client, clock: clock, tat: map[string]int64{}}
	r.degraded.Store(client == nil)
	return r
}

var rateScript = redis.NewScript(`
local now=tonumber(ARGV[1]); local interval=tonumber(ARGV[2]); local burst=tonumber(ARGV[3])
local tat=math.max(tonumber(redis.call('GET',KEYS[1]) or now),now)
local retry=tat-(burst-1)*interval-now
if retry>0 then return retry end
redis.call('SET',KEYS[1],tat+interval,'PX',math.ceil(burst*interval*2))
return 0`)

func (r *RateLimiter) Check(ctx context.Context, key string, limit int, period time.Duration) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if key == "" || limit < 1 || period < time.Millisecond {
		return 0, errors.New("invalid rate limit policy")
	}
	now := r.clock.Now().UnixMilli()
	interval := (period.Milliseconds() + int64(limit) - 1) / int64(limit)
	if r.client != nil {
		queryCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		retry, err := rateScript.Run(queryCtx, r.client, []string{"dicho:rl:" + key}, now, interval, limit).Int64()
		cancel()
		if err == nil {
			r.degraded.Store(false)
			return time.Duration(retry) * time.Millisecond, nil
		}
		if err = ctx.Err(); err != nil {
			return 0, err
		}
		r.degraded.Store(true)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tat := max(now, r.tat[key])
	retry := tat - (int64(limit)-1)*interval - now
	if retry > 0 {
		return time.Duration(retry) * time.Millisecond, nil
	}
	r.tat[key] = tat + interval
	if len(r.tat) > 10000 {
		for k, v := range r.tat {
			if v < now {
				delete(r.tat, k)
			}
		}
	}
	return 0, nil
}
func (r *RateLimiter) Degraded() bool { return r.degraded.Load() }
