package runtime

import (
	"net/http"
	"net/http/httptest"
)

// NewCompositeMetricsHandler serves OTEL Prometheus output and the hand-rolled
// runtime gauges from the same /metrics endpoint.
func NewCompositeMetricsHandler(statusHandler, prometheusHandler http.Handler) http.Handler {
	if statusHandler == nil {
		return prometheusHandler
	}
	if prometheusHandler == nil {
		return statusHandler
	}

	return compositeMetricsHandler{
		statusHandler:     statusHandler,
		prometheusHandler: prometheusHandler,
	}
}

type compositeMetricsHandler struct {
	statusHandler     http.Handler
	prometheusHandler http.Handler
}

func (h compositeMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	prometheusRecorder := httptest.NewRecorder()
	h.prometheusHandler.ServeHTTP(prometheusRecorder, r)

	statusRecorder := httptest.NewRecorder()
	h.statusHandler.ServeHTTP(statusRecorder, r)

	for key, values := range prometheusRecorder.Header() {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(prometheusRecorder.Body.Bytes())
	_, _ = w.Write([]byte("\n"))
	_, _ = w.Write(statusRecorder.Body.Bytes())
}
