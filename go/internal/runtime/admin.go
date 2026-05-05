package runtime

import (
	"fmt"
	"net/http"
	"strings"
)

// AdminCheck reports whether a runtime probe is healthy.
type AdminCheck func() error

// AdminMuxConfig defines the shared admin and probe routes for a long-running
// Go runtime.
type AdminMuxConfig struct {
	ServiceName     string
	Health          AdminCheck
	Ready           AdminCheck
	StatusHandler   http.Handler
	MetricsHandler  http.Handler
	RecoveryHandler *RecoveryHandler
}

// NewAdminMux builds the shared probe and admin route contract for a runtime.
func NewAdminMux(cfg AdminMuxConfig) (*http.ServeMux, error) {
	serviceName := strings.TrimSpace(cfg.ServiceName)
	if serviceName == "" {
		return nil, fmt.Errorf("service name is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", probeHandler(serviceName, "healthz", cfg.Health))
	mux.HandleFunc("/readyz", probeHandler(serviceName, "readyz", cfg.Ready))

	if cfg.StatusHandler != nil {
		mux.Handle("/admin/status", cfg.StatusHandler)
	}
	if cfg.MetricsHandler != nil {
		mux.Handle("/metrics", cfg.MetricsHandler)
	}
	if cfg.RecoveryHandler != nil {
		cfg.RecoveryHandler.Mount(mux)
	}

	return mux, nil
}

func probeHandler(serviceName, probeName string, check AdminCheck) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if check == nil {
			w.WriteHeader(http.StatusOK)
			if r.Method == http.MethodHead {
				return
			}
			_, _ = fmt.Fprintf(w, "service=%s probe=%s status=ok\n", serviceName, probeName)
			return
		}

		if err := check(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			if r.Method == http.MethodHead {
				return
			}
			_, _ = fmt.Fprintf(
				w,
				"service=%s probe=%s status=error error=%s\n",
				serviceName,
				probeName,
				err.Error(),
			)
			return
		}

		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		_, _ = fmt.Fprintf(w, "service=%s probe=%s status=ok\n", serviceName, probeName)
	}
}
