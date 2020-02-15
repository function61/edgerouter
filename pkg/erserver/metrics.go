package erserver

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	allAppKey = "_all"
)

type metricsStore struct {
	requestsOk      *prometheus.CounterVec
	requestsFail    *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
}

func incAppCodeMethodCounter(
	counter *prometheus.CounterVec,
	app string,
	code string,
	method string,
) {
	counter.WithLabelValues(app, code, method).Inc()
	counter.WithLabelValues(allAppKey, code, method).Inc()
}

func initMetrics() *metricsStore {
	// from 0.25ms to 8 seconds
	timeBuckets := prometheus.ExponentialBuckets(0.00025, 2, 16)

	m := &metricsStore{
		requestsOk: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "er_requests_ok",
		}, []string{"app", "code", "method"}),
		requestsFail: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "er_requests_fail",
		}, []string{"app", "code", "method"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "er_request_duration_seconds",
			Buckets: timeBuckets,
			Help:    "Histogram of the time (in seconds) each request took.",
		}, []string{"app"}),
	}

	prometheus.MustRegister(m.requestsOk)
	prometheus.MustRegister(m.requestsFail)
	prometheus.MustRegister(m.requestDuration)

	return m
}
