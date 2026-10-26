package pipelineservice

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var pipelineMetrics = struct {
	stageDuration *prometheus.HistogramVec
	stageEvents   *prometheus.CounterVec
	completed     *prometheus.CounterVec
	endToEnd      *prometheus.HistogramVec
}{
	stageDuration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "pipeline_stage_duration_seconds",
		Help: "Pipeline stage duration in seconds.",
		Buckets: []float64{
			0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60,
		},
	}, []string{"stage", "result"}),
	stageEvents: promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pipeline_stage_events_total",
		Help: "Total number of pipeline stage events.",
	}, []string{"stage", "result"}),
	completed: promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pipeline_completed_total",
		Help: "Total number of completed automatic pipeline runs.",
	}, []string{"result"}),
	endToEnd: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "pipeline_end_to_end_duration_seconds",
		Help: "Automatic pipeline duration from submission to completion in seconds.",
		Buckets: []float64{
			0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600,
		},
	}, []string{"result"}),
}

func observePipelineStage(stage, result string, duration time.Duration) {
	pipelineMetrics.stageDuration.WithLabelValues(stage, result).Observe(duration.Seconds())
	pipelineMetrics.stageEvents.WithLabelValues(stage, result).Inc()
}
