package status

import (
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTPHandlerOptions controls how the shared status report is served over HTTP.
type HTTPHandlerOptions struct {
	Now           func() time.Time
	ReportOptions Options
}

// NewHTTPHandler returns a shared HTTP adapter for the operator status report.
func NewHTTPHandler(reader Reader, opts HTTPHandlerOptions) (http.Handler, error) {
	if reader == nil {
		return nil, fmt.Errorf("status reader is required")
	}
	if opts.Now == nil {
		opts.Now = func() time.Time {
			return time.Now().UTC()
		}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveHTTP(w, r, reader, opts)
	}), nil
}

func serveHTTP(
	w http.ResponseWriter,
	r *http.Request,
	reader Reader,
	opts HTTPHandlerOptions,
) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	format := requestedHTTPFormat(r)
	if format != "text" && format != "json" {
		http.Error(w, fmt.Sprintf("unsupported format %q", format), http.StatusBadRequest)
		return
	}

	report, err := LoadReport(r.Context(), reader, opts.Now(), opts.ReportOptions)
	if err != nil {
		http.Error(
			w,
			fmt.Sprintf("load status report: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	body, contentType, err := renderHTTPBody(report, format)
	if err != nil {
		http.Error(w, fmt.Sprintf("render status report: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(body)
}

func requestedHTTPFormat(r *http.Request) string {
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format != "" {
		return format
	}

	if strings.Contains(strings.ToLower(r.Header.Get("Accept")), "application/json") {
		return "json"
	}

	return "text"
}

func renderHTTPBody(report Report, format string) ([]byte, string, error) {
	switch format {
	case "json":
		payload, err := RenderJSON(report)
		if err != nil {
			return nil, "", err
		}
		return payload, "application/json; charset=utf-8", nil
	case "text":
		return []byte(RenderText(report) + "\n"), "text/plain; charset=utf-8", nil
	default:
		return nil, "", fmt.Errorf("unsupported format %q", format)
	}
}
