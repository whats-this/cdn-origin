package prometheus

import (
	"io"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/expfmt"
)

// WriteMetrics writes data to the supplied io.Writer in the format specified by the `Accept` header (where possible).
// The `Content-Type` of the response is returned.
func WriteMetrics(writer io.Writer, acceptHeader string) (string, error) {
	metricFamilies, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return "", nil
	}

	contentType := expfmt.Negotiate(http.Header{
		"Accept": []string{acceptHeader},
	})
	enc := expfmt.NewEncoder(writer, contentType)
	for _, metricFamily := range metricFamilies {
		if err := enc.Encode(metricFamily); err != nil {
			return "", err
		}
	}
	if closer, ok := writer.(io.Closer); ok {
		closer.Close()
	}
	return string(contentType), nil
}
