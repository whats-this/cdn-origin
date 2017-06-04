package prometheus

import "github.com/prometheus/client_golang/prometheus"

// namespace for use with metric names.
const namespace = "cdn_origin"

// Additional metrics to expose, prometheus.DefaultGatherer includes runtime metrics by default.
var (
	// HTTPRequestsTotal records the total number of HTTP requests, partitioned by hostname.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: namespace,
			Name:      "http_requests_total",
			Help:      "Total number of HTTP requests, partitioned by hostname.",
		},
		[]string{"host"},
	)
)

func init() {
	// Register metrics
	prometheus.MustRegister(HTTPRequestsTotal)
}
