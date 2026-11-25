// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// seedStderr is one seed-timing record laced with sentinels in both an
// unknown key and a raw non-JSON line. None of the sentinel content may reach
// the host log.
func seedStderr() string {
	return "panic: open " + relayPathSentinel + "\n" +
		`{"component":"nvd-seed","state":"complete",` +
		`"path":"` + relayPathSentinel + `","image":"` + relayImageSentinel + `","token":"` + relayTokenSentinel + `",` +
		`"nvd_seed_read_ms":1234,"nvd_seed_buckets":7,"nvd_seed_bytes":8388608}` + "\n"
}

// seedHelper emits a result in the requested state on stdout and seedStderr
// on stderr.
func seedHelper(t *testing.T, state sdkcollector.ResultState) {
	t.Helper()
	var request Request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	item := testWorkItem()
	result := baseResult(item, state)
	switch state {
	case sdkcollector.ResultComplete:
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}
	case sdkcollector.ResultUnchanged:
		// No statuses needed; unchanged completes without fact writes.
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusComplete}}
	case sdkcollector.ResultPartial:
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusWarning}}
	case sdkcollector.ResultRetryable:
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "seed_unreadable"}}
	case sdkcollector.ResultTerminal:
		result.Statuses = []sdkcollector.Status{{Class: sdkcollector.StatusFailure, FailureClass: "seed_unreadable"}}
	}
	result.Claim = request.Claim
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		t.Fatalf("encode result: %v", err)
	}
	_, _ = os.Stderr.WriteString(seedStderr())
	os.Exit(0)
}

// TestCompleteClaimRelaysAllowlistedSeedFiguresFromSatelliteStderr proves a
// complete claim whose satellite printed its NVD seed timing puts exactly the
// three allowlisted seed figures into the host log line and that no other
// stderr content (a path, an image name, a token, a raw line) reaches it
// (eshu-hq/eshu#11387). This is the relay the Bar1 recorded-pair box in
// eshu-hq/eshu#11386 depends on: the operator reads nvd_seed_read_ms and
// nvd_seed_bytes off the `component extension result` line.
func TestCompleteClaimRelaysAllowlistedSeedFiguresFromSatelliteStderr(t *testing.T) {
	if os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") == "seed-complete" {
		seedHelper(t, sdkcollector.ResultComplete)
	}

	line, raw := relayLog(t, "seed-complete")
	for key, want := range map[string]float64{
		contract.LogKeyExtensionStatusSeedReadMS:  1234,
		contract.LogKeyExtensionStatusSeedBuckets: 7,
		contract.LogKeyExtensionStatusSeedBytes:   8388608,
	} {
		if got, ok := line[key].(float64); !ok || got != want {
			t.Fatalf("log %s = %v, want %v (line: %s)", key, line[key], want, raw)
		}
	}
	if got := line["extension_status.state"]; got != "complete" {
		t.Fatalf("extension_status.state = %v, want complete", got)
	}
	assertNoRelaySentinels(t, raw)
}

// TestNonCompleteClaimsDoNotRelaySeedFigures proves the seed relay is gated
// on a complete result: the same stderr on a terminal, retryable, partial,
// or unchanged claim logs no seed figures, through a real ProcessRunner
// child. A failed or unchanged claim performed no served seed read, so stale
// or unserved figures must never ride its line.
func TestNonCompleteClaimsDoNotRelaySeedFigures(t *testing.T) {
	switch os.Getenv("ESHU_EXTENSIONHOST_PROCESS_HELPER") {
	case "seed-terminal":
		seedHelper(t, sdkcollector.ResultTerminal)
	case "seed-retryable":
		seedHelper(t, sdkcollector.ResultRetryable)
	case "seed-partial":
		seedHelper(t, sdkcollector.ResultPartial)
	case "seed-unchanged":
		seedHelper(t, sdkcollector.ResultUnchanged)
	}

	for _, helper := range []string{"seed-terminal", "seed-retryable", "seed-partial", "seed-unchanged"} {
		t.Run(helper, func(t *testing.T) {
			line, raw := relayLog(t, helper)
			for _, key := range []string{
				contract.LogKeyExtensionStatusSeedReadMS,
				contract.LogKeyExtensionStatusSeedBuckets,
				contract.LogKeyExtensionStatusSeedBytes,
			} {
				if _, present := line[key]; present {
					t.Fatalf("%s claim logged %s, want no seed relay: %s", helper, key, raw)
				}
			}
			assertNoRelaySentinels(t, raw)
		})
	}
}

// TestParseSeedFiguresDropsHostileLines proves the seed parser drops every
// hostile shape whole: a missing, null, negative, fractional, overflowing, or
// string figure, a non-JSON line, and an over-long line never yield figures,
// while unknown keys beside a complete record are ignored.
func TestParseSeedFiguresDropsHostileLines(t *testing.T) {
	t.Parallel()

	good := `{"nvd_seed_read_ms":1234,"nvd_seed_buckets":7,"nvd_seed_bytes":8388608}`
	cases := map[string]struct {
		stderr string
		want   *SeedFigures
	}{
		"good record relays":             {good + "\n", &SeedFigures{ReadMS: 1234, Buckets: 7, Bytes: 8388608}},
		"unknown keys ignored":           {`{"nvd_seed_read_ms":1,"nvd_seed_buckets":2,"nvd_seed_bytes":3,"path":"/x","token":"y"}` + "\n", &SeedFigures{ReadMS: 1, Buckets: 2, Bytes: 3}},
		"last complete line wins":        {good + "\n" + `{"nvd_seed_read_ms":9,"nvd_seed_buckets":9,"nvd_seed_bytes":9}` + "\n", &SeedFigures{ReadMS: 9, Buckets: 9, Bytes: 9}},
		"missing key dropped":            {`{"nvd_seed_read_ms":1,"nvd_seed_buckets":2}` + "\n", nil},
		"null figure dropped":            {`{"nvd_seed_read_ms":null,"nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", nil},
		"negative figure dropped":        {`{"nvd_seed_read_ms":-1,"nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", nil},
		"fractional figure dropped":      {`{"nvd_seed_read_ms":1.5,"nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", nil},
		"string figure dropped":          {`{"nvd_seed_read_ms":"1234","nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", nil},
		"overflowing figure dropped":     {`{"nvd_seed_read_ms":99999999999999999999,"nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", nil},
		"non-JSON line dropped":          {"not json\n", nil},
		"empty stderr dropped":           {"", nil},
		"partial line then good relays":  {"not json\n" + good + "\n", &SeedFigures{ReadMS: 1234, Buckets: 7, Bytes: 8388608}},
		"good then hostile keeps good":   {good + "\n" + `{"nvd_seed_read_ms":"x","nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", &SeedFigures{ReadMS: 1234, Buckets: 7, Bytes: 8388608}},
		"large values relay":             {`{"nvd_seed_read_ms":9223372036854775807,"nvd_seed_buckets":0,"nvd_seed_bytes":0}` + "\n", &SeedFigures{ReadMS: 9223372036854775807}},
		"over-long line skipped":         {`{"nvd_seed_read_ms":1,` + strings.Repeat(" ", 5*1024) + `"nvd_seed_buckets":2,"nvd_seed_bytes":3}` + "\n", nil},
		"zero figures relay":             {`{"nvd_seed_read_ms":0,"nvd_seed_buckets":0,"nvd_seed_bytes":0}` + "\n", &SeedFigures{}},
		"seed record without key absent": {`{"inflated_bytes":1,"budget_bytes":2,"compressed_bytes":3}` + "\n", nil},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := parseSeedFigures([]byte(tc.stderr))
			if tc.want == nil {
				if got != nil {
					t.Fatalf("parseSeedFigures() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("parseSeedFigures() = nil, want %+v", tc.want)
			}
			if *got != *tc.want {
				t.Fatalf("parseSeedFigures() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// BenchmarkParseSeedFigures measures the per-complete-call relay cost: the
// typical one-line seed record, a full default stderr buffer of non-matching
// lines (the prefilter skips them), and the real worst case of short lines
// that pass the prefilter and fail Unmarshal.
func BenchmarkParseSeedFigures(b *testing.B) {
	typical := []byte(`{"nvd_seed_read_ms":1234,"nvd_seed_buckets":7,"nvd_seed_bytes":8388608}` + "\n")
	noMatch := []byte(strings.Repeat(`{"component":"nvd","state":"complete","page":0,"facts":0}`+"\n", maxScannedStderrBytes/56))
	badShort := []byte(strings.Repeat(`{"nvd_seed_read_ms":"x"}`+"\n", maxScannedStderrBytes/25))
	cases := []struct {
		name   string
		stderr []byte
	}{
		{"complete_typical", typical},
		{"complete_full_buffer_no_match", noMatch},
		{"complete_full_buffer_bad_json_short_lines", badShort},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.stderr)))
			for i := 0; i < b.N; i++ {
				_ = parseSeedFigures(tc.stderr)
			}
		})
	}
}
