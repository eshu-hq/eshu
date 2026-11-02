// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// relayHostile is stderr content a hostile or careless satellite could print.
// None of it may reach the host log.
const (
	relayPathSentinel  = "/private/rootfs/etc/shadow"
	relayImageSentinel = "registry.example.invalid/private/app@sha256:deadbeefdeadbeef"
	relayTokenSentinel = "ghs_relaysecrettoken"
)

// relayStderr is one input-limit record laced with sentinels in both an
// unknown key and a raw non-JSON line.
func relayStderr() string {
	return "panic: open " + relayPathSentinel + "\n" +
		`{"component":"image","analyzer":"image","state":"terminal","failure_class":"input_limit_exceeded",` +
		`"path":"` + relayPathSentinel + `","image":"` + relayImageSentinel + `","token":"` + relayTokenSentinel + `",` +
		`"inflated_bytes":34359738369,"budget_bytes":34359738368,"compressed_bytes":1077449478}` + "\n"
}

// relayHelper emits a terminal input_limit_exceeded result on stdout and
// relayStderr on stderr, or a complete result with the same stderr when the
// helper flag asks for it.
func relayHelper(t *testing.T, state sdkcollector.ResultState) {
	t.Helper()
	var request Request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	item := testWorkItem()
	result := baseResult(item, state)
	if state == sdkcollector.ResultTerminal {
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "input_limit_exceeded"}}
	} else {
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}
	}
	result.Claim = request.Claim
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatalf("encode result: %v", err)
	}
	_, _ = os.Stderr.WriteString(relayStderr())
	os.Exit(0)
}

// relayLog runs one claim through a real ProcessRunner satellite and returns
// the one `component extension result` log line, decoded.
func relayLog(t *testing.T, helper string) (map[string]any, string) {
	t.Helper()
	item := testWorkItem()
	item.LeaseExpiresAt = time.Now().Add(time.Minute)
	runner := ProcessRunner{
		Command: os.Args[0],
		Args:    []string{"-test.run=^" + t.Name() + "$"},
		Env:     []string{"ESHU_EXTENSIONHOST_PROCESS_HELPER=" + helper},
	}
	var logs bytes.Buffer
	telemetry := NewStatusTelemetry(nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	source := mustNewSource(t, runner, telemetry)
	_, _, _ = source.NextClaimed(context.Background(), item)

	raw := strings.TrimSpace(logs.String())
	if raw == "" {
		t.Fatal("no log line written")
	}
	lines := strings.Split(raw, "\n")
	if len(lines) != 1 {
		t.Fatalf("log lines = %d, want exactly 1 (one relay line per claim): %s", len(lines), raw)
	}
	var line map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &line); err != nil {
		t.Fatalf("log line is not JSON: %v", err)
	}
	return line, raw
}

func assertNoRelaySentinels(t *testing.T, raw string) {
	t.Helper()
	for _, sentinel := range []string{relayPathSentinel, relayImageSentinel, relayTokenSentinel, "private", "panic", "shadow"} {
		if strings.Contains(raw, sentinel) {
			t.Fatalf("host log leaked satellite content %q: %s", sentinel, raw)
		}
	}
}

// TestFailedClaimRelaysAllowlistedFiguresFromSatelliteStderr proves a terminal
// input_limit_exceeded claim puts exactly the three allowlisted figures into
// the host log line and that no other stderr content (a path, an image name,
// a token, a raw line) reaches it (eshu-hq/eshu#9823).
func TestFailedClaimRelaysAllowlistedFiguresFromSatelliteStderr(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "relay-terminal" {
		relayHelper(t, sdkcollector.ResultTerminal)
	}

	line, raw := relayLog(t, "relay-terminal")
	for key, want := range map[string]float64{
		contract.LogKeyExtensionStatusInflatedBytes:   34359738369,
		contract.LogKeyExtensionStatusBudgetBytes:     34359738368,
		contract.LogKeyExtensionStatusCompressedBytes: 1077449478,
	} {
		if got, ok := line[key].(float64); !ok || got != want {
			t.Fatalf("log %s = %v, want %v (line: %s)", key, line[key], want, raw)
		}
	}
	if got := line["failure_class"]; got != "input_limit_exceeded" {
		t.Fatalf("failure_class = %v, want input_limit_exceeded", got)
	}
	assertNoRelaySentinels(t, raw)
}

// TestCompleteAndUnchangedClaimsDoNotRelaySatelliteStderr proves the relay is
// gated on a non-complete, non-unchanged result: the same stderr on a complete
// or an unchanged claim logs no figures, through a real ProcessRunner child.
func TestCompleteAndUnchangedClaimsDoNotRelaySatelliteStderr(t *testing.T) {
	switch os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") {
	case "relay-complete":
		relayHelper(t, sdkcollector.ResultComplete)
	case "relay-unchanged":
		relayHelper(t, sdkcollector.ResultUnchanged)
	}

	for _, helper := range []string{"relay-complete", "relay-unchanged"} {
		t.Run(helper, func(t *testing.T) {
			line, raw := relayLog(t, helper)
			for _, key := range []string{
				contract.LogKeyExtensionStatusInflatedBytes,
				contract.LogKeyExtensionStatusBudgetBytes,
				contract.LogKeyExtensionStatusCompressedBytes,
			} {
				if _, present := line[key]; present {
					t.Fatalf("%s claim logged %s, want no relay: %s", helper, key, raw)
				}
			}
			assertNoRelaySentinels(t, raw)
		})
	}
}

// TestOCIRunnerReturnsFiguresOnlyForNonCompleteCalls proves the OCI runner
// parses stderr into typed figures for a terminal, retryable, or partial call,
// drops it for a complete or unchanged call, and never exposes raw stderr on
// the call result.
func TestOCIRunnerReturnsFiguresOnlyForNonCompleteCalls(t *testing.T) {
	t.Parallel()

	for state, wantFigures := range map[sdkcollector.ResultState]bool{
		sdkcollector.ResultTerminal:  true,
		sdkcollector.ResultRetryable: true,
		sdkcollector.ResultPartial:   true,
		sdkcollector.ResultComplete:  false,
		sdkcollector.ResultUnchanged: false,
	} {
		runner := OCIRunner{
			ImageRef: ociTestDigest,
			exec: func(_ context.Context, _ string, _ []string, _ []byte, stdout, stderr io.Writer) error {
				_, _ = stdout.Write([]byte(`{"protocol_version":"` + sdkcollector.ProtocolVersionV1Alpha1 + `","state":"` + string(state) + `"}`))
				_, _ = stderr.Write([]byte(relayStderr()))
				return nil
			},
		}
		call, err := runner.RunPagedCollector(context.Background(), ociTestRequest())
		if err != nil {
			t.Fatalf("state %s: RunPagedCollector() error = %v", state, err)
		}
		if got := call.InputLimit != nil; got != wantFigures {
			t.Fatalf("state %s: InputLimit present = %t, want %t", state, got, wantFigures)
		}
		if wantFigures && *call.InputLimit != (InputLimitFigures{InflatedBytes: 34359738369, BudgetBytes: 34359738368, CompressedBytes: 1077449478}) {
			t.Fatalf("state %s: InputLimit = %+v", state, *call.InputLimit)
		}
	}
}

// figuresPagedRunner is a PagedRunner that hands back a fixed call result.
type figuresPagedRunner struct {
	call CallResult
}

func (r figuresPagedRunner) RunCollector(context.Context, Request) (sdkcollector.Result, error) {
	return r.call.Result, nil
}

func (r figuresPagedRunner) RunPagedCollector(context.Context, Request) (CallResult, error) {
	return r.call, nil
}

// TestRelayReachesResultRecordOnlyForValidatedSDKResults proves the figures
// ride on the one ResultRecord of a validated SDK terminal result, and that a
// host verdict (here an identity mismatch) never carries satellite figures
// even when the runner produced them.
func TestRelayReachesResultRecordOnlyForValidatedSDKResults(t *testing.T) {
	t.Parallel()

	figures := &InputLimitFigures{InflatedBytes: 9, BudgetBytes: 8, CompressedBytes: 2}
	tests := []struct {
		name       string
		mismatch   bool
		wantOrigin ResultOrigin
		wantFigure *InputLimitFigures
	}{
		{name: "validated terminal result", wantOrigin: ResultOriginSDK, wantFigure: figures},
		{name: "host identity mismatch", mismatch: true, wantOrigin: ResultOriginHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			item := testWorkItem()
			result := baseResult(item, sdkcollector.ResultTerminal)
			result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "input_limit_exceeded"}}
			if tt.mismatch {
				result.Claim.WorkItemID = "work-someone-else"
			}
			recorder := &recordingResultRecorder{}
			source := mustNewSource(t, figuresPagedRunner{call: CallResult{Result: result, InputLimit: figures}}, recorder)
			_, _, _ = source.NextClaimed(context.Background(), item)
			if len(recorder.results) != 1 {
				t.Fatalf("results = %d, want 1", len(recorder.results))
			}
			record := recorder.results[0]
			if record.Origin != tt.wantOrigin {
				t.Fatalf("origin = %q, want %q", record.Origin, tt.wantOrigin)
			}
			if (record.InputLimit == nil) != (tt.wantFigure == nil) || (record.InputLimit != nil && *record.InputLimit != *tt.wantFigure) {
				t.Fatalf("InputLimit = %+v, want %+v", record.InputLimit, tt.wantFigure)
			}
		})
	}
}
