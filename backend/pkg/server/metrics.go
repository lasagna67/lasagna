package server

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var requestMetrics = struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight *prometheus.GaugeVec
}{
	requests: promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total number of HTTP requests.",
	}, []string{"method", "route", "status"}),
	duration: promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "http_request_duration_seconds",
		Help: "HTTP request duration in seconds.",
	}, []string{"method", "route", "status"}),
	inFlight: promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Current number of HTTP requests being served.",
	}, []string{"method", "route"}),
}

func observeRequest(method, route string, status int, duration time.Duration) {
	labels := []string{method, route, strconv.Itoa(status)}
	requestMetrics.requests.WithLabelValues(labels...).Inc()
	requestMetrics.duration.WithLabelValues(labels...).Observe(duration.Seconds())
}
