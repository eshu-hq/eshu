package runtime

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewPprofServerReturnsNilWhenEnvUnset(t *testing.T) {
	t.Parallel()

	server, err := NewPprofServer(func(string) string { return "" })
	if err != nil {
		t.Fatalf("NewPprofServer() error = %v, want nil", err)
	}
	if server != nil {
		t.Fatalf("NewPprofServer() = %v, want nil when ESHU_PPROF_ADDR is unset", server)
	}
}

func TestNewPprofServerReturnsNilWhenEnvBlank(t *testing.T) {
	t.Parallel()

	server, err := NewPprofServer(func(string) string { return "   " })
	if err != nil {
		t.Fatalf("NewPprofServer() error = %v, want nil", err)
	}
	if server != nil {
		t.Fatal("NewPprofServer() with whitespace-only addr = non-nil, want nil")
	}
}

func TestNewPprofServerRejectsInvalidAddr(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"garbage", "host:port:extra"} {
		addr := addr
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			_, err := NewPprofServer(func(string) string { return addr })
			if err == nil {
				t.Fatalf("NewPprofServer(%q) error = nil, want non-nil", addr)
			}
		})
	}
}

func TestNewPprofServerBindsLoopbackWhenHostMissing(t *testing.T) {
	t.Parallel()

	server, err := NewPprofServer(func(string) string { return ":0" })
	if err != nil {
		t.Fatalf("NewPprofServer() error = %v, want nil", err)
	}
	if server == nil {
		t.Fatal("NewPprofServer() = nil, want server")
	}

	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = server.Stop(context.Background())
	})

	addr := server.Addr()
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("Addr() = %q, want 127.0.0.1: prefix (loopback default)", addr)
	}
}

// TestNormalizePprofAddr covers the host/port normalization contract directly
// instead of going through Start(). When 0.0.0.0 binds on a dual-stack host,
// the OS reports the bound address as [::], which makes a socket-level
// assertion on "0.0.0.0 is preserved" brittle. The configured address is the
// guarantee we need to make at the helper boundary.
func TestNormalizePprofAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "empty disables", in: "", want: ""},
		{name: "whitespace disables", in: "   ", want: ""},
		{name: "bare port forces loopback", in: ":6060", want: "127.0.0.1:6060"},
		{name: "bare zero port forces loopback", in: ":0", want: "127.0.0.1:0"},
		{name: "loopback explicit preserved", in: "127.0.0.1:6060", want: "127.0.0.1:6060"},
		{name: "wildcard host preserved", in: "0.0.0.0:6060", want: "0.0.0.0:6060"},
		{name: "named host preserved", in: "localhost:6060", want: "localhost:6060"},
		{name: "ipv6 loopback preserved", in: "[::1]:6060", want: "[::1]:6060"},
		{name: "no colon rejected", in: "garbage", wantErr: true},
		{name: "extra colon rejected", in: "host:port:extra", wantErr: true},
		{name: "empty port rejected", in: ":", wantErr: true},
		{name: "non-numeric port rejected", in: ":abc", wantErr: true},
		{name: "negative port rejected", in: ":-1", wantErr: true},
		{name: "port above 65535 rejected", in: ":65536", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizePprofAddr(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("normalizePprofAddr(%q) error = nil, want non-nil", tc.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizePprofAddr(%q) error = %v, want nil", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizePprofAddr(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNewPprofServerServesDebugPprofRoutes(t *testing.T) {
	t.Parallel()

	server, err := NewPprofServer(func(string) string { return "127.0.0.1:0" })
	if err != nil {
		t.Fatalf("NewPprofServer() error = %v, want nil", err)
	}
	if server == nil {
		t.Fatal("NewPprofServer() = nil, want server")
	}
	if err := server.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v, want nil", err)
	}
	t.Cleanup(func() {
		_ = server.Stop(context.Background())
	})

	client := &http.Client{Timeout: 2 * time.Second}

	// Index page must register at /debug/pprof/.
	indexURL := "http://" + server.Addr() + "/debug/pprof/"
	resp, err := client.Get(indexURL)
	if err != nil {
		t.Fatalf("GET %s error = %v, want nil", indexURL, err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", indexURL, resp.StatusCode)
	}
	if !strings.Contains(string(body), "Types of profiles available") {
		t.Fatalf("GET %s body missing pprof index marker; got %q", indexURL, truncate(string(body), 200))
	}

	// /debug/pprof/cmdline proves named handlers registered, not just the
	// catch-all index. Returns the test binary's cmdline as a NUL-delimited
	// stream — must be non-empty and 200.
	cmdlineURL := "http://" + server.Addr() + "/debug/pprof/cmdline"
	resp2, err := client.Get(cmdlineURL)
	if err != nil {
		t.Fatalf("GET %s error = %v, want nil", cmdlineURL, err)
	}
	cmdlineBody, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", cmdlineURL, resp2.StatusCode)
	}
	if len(cmdlineBody) == 0 {
		t.Fatalf("GET %s body is empty, want non-empty cmdline", cmdlineURL)
	}
}

func TestNewPprofServerReadsCanonicalEnvVar(t *testing.T) {
	t.Parallel()

	got := ""
	server, err := NewPprofServer(func(name string) string {
		got = name
		return ""
	})
	if err != nil {
		t.Fatalf("NewPprofServer() error = %v, want nil", err)
	}
	if server != nil {
		t.Fatal("NewPprofServer() = non-nil, want nil for empty env")
	}
	if got != "ESHU_PPROF_ADDR" {
		t.Fatalf("getenv called with %q, want %q", got, "ESHU_PPROF_ADDR")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
