package observability

import (
	"context"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type QueueGroup struct {
	Kind, State      string
	Jobs             int64
	OldestDueSeconds float64
}

type QueueSnapshot struct{ Groups []QueueGroup }

var queueStates = [...]string{"available", "pending", "retryable", "running", "scheduled", "discarded"}

type queueCollector struct {
	read          func(context.Context) (QueueSnapshot, error)
	up, jobs, age *prometheus.Desc
}

func newQueueCollector(read func(context.Context) (QueueSnapshot, error)) *queueCollector {
	return &queueCollector{
		read: read,
		up:   prometheus.NewDesc("dicho_queue_stats_up", "Whether the current River queue snapshot was collected successfully.", nil, nil),
		jobs: prometheus.NewDesc("dicho_queue_jobs", "Retained active or discarded River jobs by bounded event kind and state.", []string{"kind", "state"}, nil),
		age:  prometheus.NewDesc("dicho_queue_oldest_due_seconds", "Seconds since the oldest waiting River job became due.", []string{"kind"}, nil),
	}
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.up; ch <- c.jobs; ch <- c.age }

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	snapshot, err := c.read(ctx)
	groups := make(map[[2]string]QueueGroup)
	valid := err == nil && ctx.Err() == nil
	for _, group := range snapshot.Groups {
		knownState := false
		for _, state := range queueStates {
			if group.State == state {
				knownState = true
				break
			}
		}
		if !knownState || group.Jobs < 0 || group.OldestDueSeconds < 0 || math.IsNaN(group.OldestDueSeconds) || math.IsInf(group.OldestDueSeconds, 0) {
			valid = false
			break
		}
		group.Kind = JobKind(group.Kind)
		key := [2]string{group.Kind, group.State}
		old := groups[key]
		if group.Jobs > math.MaxInt64-old.Jobs {
			valid = false
			break
		}
		group.Jobs += old.Jobs
		group.OldestDueSeconds = max(group.OldestDueSeconds, old.OldestDueSeconds)
		groups[key] = group
	}
	if !valid {
		ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 0)
		return
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, 1)
	for _, kind := range JobKinds() {
		var oldest float64
		for _, state := range queueStates {
			group := groups[[2]string{kind, state}]
			ch <- prometheus.MustNewConstMetric(c.jobs, prometheus.GaugeValue, float64(group.Jobs), kind, state)
			if state == "available" || state == "retryable" || state == "scheduled" {
				oldest = max(oldest, group.OldestDueSeconds)
			}
		}
		ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, oldest, kind)
	}
}
