package pipelineservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	applicationdomain "github.com/lasagna67/lasagna.git/internal/application/domain"
	companydomain "github.com/lasagna67/lasagna.git/internal/company/domain"
	pipelinedomain "github.com/lasagna67/lasagna.git/internal/pipeline/domain"
	"github.com/lasagna67/lasagna.git/pkg/kafka"
	"github.com/lasagna67/lasagna.git/pkg/logger"
	"github.com/prometheus/client_golang/prometheus"
)

func TestPipelineMetricsAndLogs(t *testing.T) {
	e := newEnv()
	var logs bytes.Buffer
	e.svc.logger = &logger.Logger{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	d := e.submitted()
	submittedAt := fixedNow.Add(-5 * time.Second)
	stored := e.repo.apps[d.ID]
	stored.SubmittedAt = &submittedAt
	e.repo.apps[d.ID] = stored

	stageCounts := map[string]uint64{}
	for _, stage := range []string{"validation", "enrichment", "scoring"} {
		stageCounts[stage] = histogramCount(t, "pipeline_stage_duration_seconds", map[string]string{"stage": stage, "result": "success"})
	}
	completedBefore := counterValue(t, "pipeline_completed_total", map[string]string{"result": string(applicationdomain.StatusManualReview)})
	endToEndBefore := histogramCount(t, "pipeline_end_to_end_duration_seconds", map[string]string{"result": string(applicationdomain.StatusManualReview)})

	e.repo.deliver(t, e.svc)

	for _, stage := range []string{"validation", "enrichment", "scoring"} {
		labels := map[string]string{"stage": stage, "result": "success"}
		if got := counterValue(t, "pipeline_stage_events_total", labels); got < 1 {
			t.Errorf("%s success events = %v", stage, got)
		}
		if got := histogramCount(t, "pipeline_stage_duration_seconds", labels); got != stageCounts[stage]+1 {
			t.Errorf("%s duration observations = %d; want %d", stage, got, stageCounts[stage]+1)
		}
	}
	resultLabels := map[string]string{"result": string(applicationdomain.StatusManualReview)}
	if got := counterValue(t, "pipeline_completed_total", resultLabels); got != completedBefore+1 {
		t.Errorf("completed = %v; want %v", got, completedBefore+1)
	}
	if got := histogramCount(t, "pipeline_end_to_end_duration_seconds", resultLabels); got != endToEndBefore+1 {
		t.Errorf("end-to-end observations = %d; want %d", got, endToEndBefore+1)
	}

	var stages []string
	for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode log: %v", err)
		}
		if record["msg"] != "pipeline stage completed" {
			continue
		}
		if record["application_id"] != d.ID.String() || record["run_id"] != d.ProcessingRunID.String() || record["result"] != "success" {
			t.Errorf("stage log = %v", record)
		}
		if _, ok := record["duration_ms"]; !ok {
			t.Errorf("stage log has no duration_ms: %v", record)
		}
		stages = append(stages, record["stage"].(string))
	}
	if len(stages) != 3 {
		t.Fatalf("stage logs = %v", stages)
	}
}

func TestPipelineMetricsRecordErrorsAndSkips(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		e := newEnv()
		d := e.submitted()
		e.repo.updateErr = errors.New("database unavailable")
		labels := map[string]string{"stage": "validation", "result": "error"}
		before := counterValue(t, "pipeline_stage_events_total", labels)

		msg := pipelinedomain.Event{ApplicationID: d.ID, RunID: *d.ProcessingRunID}.Message(pipelinedomain.TopicSubmitted)
		if err := e.svc.Handle(context.Background(), kafka.Message{Topic: msg.Topic, Value: msg.Payload}); err == nil {
			t.Fatal("expected error")
		}
		if got := counterValue(t, "pipeline_stage_events_total", labels); got != before+1 {
			t.Errorf("error events = %v; want %v", got, before+1)
		}
	})

	t.Run("skipped", func(t *testing.T) {
		e := newEnv()
		d := e.submitted()
		labels := map[string]string{"stage": "validation", "result": "skipped"}
		before := counterValue(t, "pipeline_stage_events_total", labels)

		msg := pipelinedomain.Event{ApplicationID: d.ID, RunID: uuid.New()}.Message(pipelinedomain.TopicSubmitted)
		if err := e.svc.Handle(context.Background(), kafka.Message{Topic: msg.Topic, Value: msg.Payload}); err != nil {
			t.Fatalf("handle skipped event: %v", err)
		}
		if got := counterValue(t, "pipeline_stage_events_total", labels); got != before+1 {
			t.Errorf("skipped events = %v; want %v", got, before+1)
		}
	})
}

func TestPipelineCompletionIsRecordedByTerminalStage(t *testing.T) {
	t.Run("validation to manual review", func(t *testing.T) {
		e := newEnv()
		d := e.submitted()
		stored := e.repo.apps[d.ID]
		stored.Company.INN = ""
		e.repo.apps[d.ID] = stored
		labels := map[string]string{"result": string(applicationdomain.StatusManualReview)}
		before := counterValue(t, "pipeline_completed_total", labels)

		e.repo.deliver(t, e.svc)

		if got := counterValue(t, "pipeline_completed_total", labels); got != before+1 {
			t.Errorf("completed = %v; want %v", got, before+1)
		}
		if e.repo.gets != 0 {
			t.Errorf("validation completion performed %d post-stage reads", e.repo.gets)
		}
	})

	t.Run("enrichment to rejected", func(t *testing.T) {
		e := newEnv()
		e.sources.registry = []func() (companydomain.RegistryCard, error){func() (companydomain.RegistryCard, error) {
			return card(companydomain.StatusTerminated), nil
		}}
		e.submitted()
		labels := map[string]string{"result": string(applicationdomain.StatusRejected)}
		before := counterValue(t, "pipeline_completed_total", labels)

		e.repo.deliver(t, e.svc)

		if got := counterValue(t, "pipeline_completed_total", labels); got != before+1 {
			t.Errorf("completed = %v; want %v", got, before+1)
		}
	})
}

func TestPipelineMetricFamiliesAreRegistered(t *testing.T) {
	pipelineMetrics.stageDuration.WithLabelValues("validation", "success")
	pipelineMetrics.stageEvents.WithLabelValues("validation", "success")
	pipelineMetrics.completed.WithLabelValues(string(applicationdomain.StatusManualReview))
	pipelineMetrics.endToEnd.WithLabelValues(string(applicationdomain.StatusManualReview))

	want := map[string]bool{
		"pipeline_stage_duration_seconds":      false,
		"pipeline_stage_events_total":          false,
		"pipeline_completed_total":             false,
		"pipeline_end_to_end_duration_seconds": false,
	}
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if _, ok := want[family.GetName()]; ok {
			want[family.GetName()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("metric %s is not registered", name)
		}
	}
}

func counterValue(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	metric := gatheredMetric(t, name, labels)
	return metric.counter
}

func histogramCount(t *testing.T, name string, labels map[string]string) uint64 {
	t.Helper()
	metric := gatheredMetric(t, name, labels)
	return metric.histogramCount
}

type metricSnapshot struct {
	counter        float64
	histogramCount uint64
}

func gatheredMetric(t *testing.T, name string, labels map[string]string) metricSnapshot {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.Metric {
			matched := 0
			for _, pair := range metric.Label {
				if labels[pair.GetName()] == pair.GetValue() {
					matched++
				}
			}
			if matched == len(labels) {
				return metricSnapshot{
					counter:        metric.GetCounter().GetValue(),
					histogramCount: metric.GetHistogram().GetSampleCount(),
				}
			}
		}
	}
	return metricSnapshot{}
}
