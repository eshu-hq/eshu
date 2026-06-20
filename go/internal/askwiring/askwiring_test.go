package askwiring_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/askwiring"
)

func TestIsAskEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"empty", "", false},
		{"false", "false", false},
		{"true lowercase", "true", true},
		{"true uppercase", "TRUE", true},
		{"true mixed", "True", true},
		{"whitespace true", "  true  ", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := askwiring.IsAskEnabled(func(key string) string {
				if key == askwiring.EnvAskEnabled {
					return tc.envVal
				}
				return ""
			})
			if got != tc.want {
				t.Fatalf("IsAskEnabled(%q) = %v, want %v", tc.envVal, got, tc.want)
			}
		})
	}
}

func TestIsNarrationEnabled(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		envVal string
		want   bool
	}{
		{"empty", "", false},
		{"false", "false", false},
		{"true", "true", true},
		{"TRUE", "TRUE", true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := askwiring.IsNarrationEnabled(func(key string) string {
				if key == askwiring.EnvAskNarrationEnabled {
					return tc.envVal
				}
				return ""
			})
			if got != tc.want {
				t.Fatalf("IsNarrationEnabled(%q) = %v, want %v", tc.envVal, got, tc.want)
			}
		})
	}
}

// TestBuildAskHandlerDefaultOff proves that BuildAskHandler returns a
// default-off handler (nil Asker) when ESHU_ASK_ENABLED is unset or false.
func TestBuildAskHandlerDefaultOff(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(string) string { return "" },
		http.NewServeMux(),
		"",
		nil,
	)

	if result.Handler == nil {
		t.Fatal("BuildAskHandler() Handler = nil, want non-nil")
	}
	if result.AdapterReady() {
		t.Fatal("BuildAskHandler() AdapterReady() = true, want false when disabled")
	}
	if result.SetPosture == nil {
		t.Fatal("BuildAskHandler() SetPosture = nil, want non-nil no-op func")
	}
}

// TestBuildAskHandlerDefaultOffNoProfileConfigured proves that even when
// ESHU_ASK_ENABLED=true but no agent_reasoning profile exists, the handler
// remains default-off.
func TestBuildAskHandlerDefaultOffNoProfileConfigured(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(key string) string {
			if key == askwiring.EnvAskEnabled {
				return "true"
			}
			return ""
		},
		http.NewServeMux(),
		"",
		nil,
	)

	if result.AdapterReady() {
		t.Fatal("BuildAskHandler() AdapterReady() = true, want false when no profile configured")
	}
}

// TestBuildAskHandlerDefaultOffSetPostureIsNoop proves SetPosture does not
// panic when called on a default-off result.
func TestBuildAskHandlerDefaultOffSetPostureIsNoop(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(string) string { return "" },
		http.NewServeMux(),
		"",
		nil,
	)

	// Must not panic.
	result.SetPosture(nil)
}

// TestBuildNarrationPostureDefaultClosed proves the posture function is
// default-closed when no env vars are set.
func TestBuildNarrationPostureDefaultClosed(t *testing.T) {
	t.Parallel()

	posture := askwiring.BuildNarrationPosture(func(string) string { return "" }, false)
	if posture == nil {
		t.Fatal("BuildNarrationPosture() = nil, want non-nil func")
	}

	got := posture()
	// When nothing is configured the posture must not be Available.
	if strings.EqualFold(got.State, "available") {
		t.Fatalf("BuildNarrationPosture() returned Available when nothing configured; want closed")
	}
}

// TestBuildAskHandlerRouteIsRegistered proves that the handler returned by
// BuildAskHandler can be mounted and replies to POST /api/v0/ask (503, not
// 404) when in default-off mode — mirroring the integration test in
// cmd/mcp-server/ask_wiring_test.go.
func TestBuildAskHandlerRouteIsRegistered(t *testing.T) {
	t.Parallel()

	result := askwiring.BuildAskHandler(
		func(string) string { return "" },
		http.NewServeMux(),
		"",
		nil,
	)

	mux := http.NewServeMux()
	result.Handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/ask",
		strings.NewReader(`{"question":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /api/v0/ask returned 404; AskHandler route not mounted")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/v0/ask status = %d, want 503 (default-off)", rec.Code)
	}
}

// TestResolveAgentReasoningProfileEmptyEnv proves the function returns
// (zero, false) when no profile env var is set.
func TestResolveAgentReasoningProfileEmptyEnv(t *testing.T) {
	t.Parallel()

	_, ok := askwiring.ResolveAgentReasoningProfile(func(string) string { return "" }, nil)
	if ok {
		t.Fatal("ResolveAgentReasoningProfile() = _, true; want false when env is empty")
	}
}
