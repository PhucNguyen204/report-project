package observability

import (
	"context"
	"errors"
	"math"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func queueSamples(t *testing.T, o *Observer) (map[string]float64, string) {
	t.Helper()
	w := httptest.NewRecorder()
	o.Metrics().ServeHTTP(w, httptest.NewRequest("GET", "/metrics", nil))
	if w.Code != 200 {
		t.Fatalf("scrape HTTP%d", w.Code)
	}
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(w.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	samples := map[string]float64{}
	for name, family := range families {
		for _, metric := range family.Metric {
			key := name
			labels := map[string]string{}
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			if kind, ok := labels["kind"]; ok {
				key += ":" + kind
			}
			if state, ok := labels["state"]; ok {
				key += ":" + state
			}
			samples[key] = metric.GetGauge().GetValue()
		}
	}
	return samples, w.Body.String()
}

func TestQueueMetricsExposeBoundedCountsAndDueAge(t *testing.T) {
	o := New(Options{QueueStats: func(context.Context) (QueueSnapshot, error) {
		return QueueSnapshot{Groups: []QueueGroup{
			{Kind: "email", State: "available", Jobs: 2, OldestDueSeconds: 90},
			{Kind: "email", State: "retryable", Jobs: 1, OldestDueSeconds: 120},
			{Kind: "email", State: "scheduled", Jobs: 3},
			{Kind: "email", State: "running", Jobs: 1, OldestDueSeconds: 1000},
			{Kind: "ai.suggest", State: "pending", Jobs: 1},
			{Kind: "PRIVATE-ID-A", State: "discarded", Jobs: 1},
			{Kind: "PRIVATE-ID-B", State: "discarded", Jobs: 2},
		}}, nil
	}})
	samples, raw := queueSamples(t, o)
	for key, want := range map[string]float64{
		"dicho_queue_stats_up":                      1,
		"dicho_queue_jobs:email:available":          2,
		"dicho_queue_jobs:email:scheduled":          3,
		"dicho_queue_jobs:notification:available":   0,
		"dicho_queue_jobs:other:discarded":          3,
		"dicho_queue_jobs:ai.suggest:pending":       1,
		"dicho_queue_oldest_due_seconds:email":      120,
		"dicho_queue_oldest_due_seconds:ai.suggest": 0,
	} {
		got, ok := samples[key]
		if !ok || got != want {
			t.Fatalf("%s got%v present%v want%v", key, got, ok, want)
		}
	}
	if strings.Contains(raw, "PRIVATE") {
		t.Fatal("unbounded job label leaked")
	}
}

func TestQueueMetricsDoNotKeepStaleSuccessAfterErrorOrEmptyQueue(t *testing.T) {
	var snapshot = QueueSnapshot{Groups: []QueueGroup{{Kind: "email", State: "available", Jobs: 1, OldestDueSeconds: 60}}}
	var failure error
	o := New(Options{QueueStats: func(context.Context) (QueueSnapshot, error) { return snapshot, failure }})
	first, _ := queueSamples(t, o)
	if first["dicho_queue_jobs:email:available"] != 1 {
		t.Fatal("initial queue missing")
	}
	snapshot = QueueSnapshot{}
	empty, _ := queueSamples(t, o)
	if empty["dicho_queue_jobs:email:available"] != 0 || empty["dicho_queue_oldest_due_seconds:email"] != 0 || empty["dicho_queue_stats_up"] != 1 {
		t.Fatal("empty queue retained stale values")
	}
	failure = errors.New("PRIVATE-DSN-error")
	failed, raw := queueSamples(t, o)
	up, present := failed["dicho_queue_stats_up"]
	if !present || up != 0 || strings.Contains(raw, "dicho_queue_jobs{") || strings.Contains(raw, "dicho_queue_oldest_due_seconds{") || strings.Contains(raw, "PRIVATE") {
		t.Fatal("failed source emitted healthy or private samples")
	}
}

func TestQueueMetricsRejectInvalidSnapshotWithoutPartialSamples(t *testing.T) {
	for _, bad := range []QueueGroup{
		{Kind: "email", State: "available", Jobs: -1},
		{Kind: "email", State: "completed", Jobs: 1},
		{Kind: "email", State: "available", Jobs: 1, OldestDueSeconds: -1},
		{Kind: "email", State: "available", Jobs: 1, OldestDueSeconds: math.NaN()},
		{Kind: "email", State: "available", Jobs: 1, OldestDueSeconds: math.Inf(1)},
	} {
		o := New(Options{QueueStats: func(context.Context) (QueueSnapshot, error) {
			return QueueSnapshot{Groups: []QueueGroup{{Kind: "notification", State: "available", Jobs: 1}, bad}}, nil
		}})
		samples, raw := queueSamples(t, o)
		up, present := samples["dicho_queue_stats_up"]
		if !present || up != 0 || strings.Contains(raw, "dicho_queue_jobs{") {
			t.Fatalf("bad snapshot partially published: %+v", bad)
		}
	}
}

func TestQueueMetricsRejectCountOverflowWhenCombiningUnknownKinds(t *testing.T) {
	o := New(Options{QueueStats: func(context.Context) (QueueSnapshot, error) {
		return QueueSnapshot{Groups: []QueueGroup{
			{Kind: "PRIVATE-A", State: "available", Jobs: math.MaxInt64},
			{Kind: "PRIVATE-B", State: "available", Jobs: 1},
		}}, nil
	}})
	samples, raw := queueSamples(t, o)
	up, present := samples["dicho_queue_stats_up"]
	if !present || up != 0 || strings.Contains(raw, "dicho_queue_jobs{") {
		t.Fatal("overflowed count was emitted as healthy")
	}
}

func TestQueueMetricsBoundCanceledSourceAndAllowConcurrentScrapes(t *testing.T) {
	var deadline time.Duration
	o := New(Options{QueueStats: func(ctx context.Context) (QueueSnapshot, error) {
		d, ok := ctx.Deadline()
		if !ok {
			return QueueSnapshot{}, errors.New("missing deadline")
		}
		deadline = time.Until(d)
		<-ctx.Done()
		return QueueSnapshot{}, ctx.Err()
	}})
	started := time.Now()
	samples, _ := queueSamples(t, o)
	if deadline <= 0 || deadline > 250*time.Millisecond || time.Since(started) > time.Second || samples["dicho_queue_stats_up"] != 0 {
		t.Fatal("source deadline missing or scrape blocked")
	}
	concurrent := New(Options{QueueStats: func(context.Context) (QueueSnapshot, error) {
		return QueueSnapshot{Groups: []QueueGroup{{Kind: "email", State: "available", Jobs: 2}}}, nil
	}})
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			if _, err := concurrent.registry.Gather(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	last, _ := queueSamples(t, concurrent)
	if last["dicho_queue_jobs:email:available"] != 2 {
		t.Fatal("concurrent scrape lost data")
	}
}

func TestObserverWithoutQueueSourceDoesNotClaimQueueHealth(t *testing.T) {
	_, raw := queueSamples(t, New(Options{}))
	if strings.Contains(raw, "dicho_queue_") {
		t.Fatal("missing source was reported as a healthy queue")
	}
}
