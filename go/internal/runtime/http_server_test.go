package runtime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPServerRequiresAddress(t *testing.T) {
	t.Parallel()

	_, err := NewHTTPServer(HTTPServerConfig{})
	if err == nil {
		t.Fatal("NewHTTPServer() error = nil, want non-nil")
	}
}

func TestHTTPServerServesConfiguredHandlerAndShutsDown(t *testing.T) {
	t.Parallel()

	server, err := NewHTTPServer(HTTPServerConfig{
		Addr: "127.0.0.1:0",
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}),
		ShutdownTimeout: time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPServer() error = %v, want nil", err)
	}

	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}

	response, err := http.Get("http://" + server.Addr() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz error = %v, want nil", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("ReadAll() error = %v, want nil", err)
	}
	if got, want := strings.TrimSpace(string(body)), "ok"; got != want {
		t.Fatalf("GET /healthz body = %q, want %q", got, want)
	}

	if err := server.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v, want nil", err)
	}
}
