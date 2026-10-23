// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schedule

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseScanInterval(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		raw     string
		want    time.Duration
		wantSet bool
		wantErr string
	}{
		{name: "empty document", raw: "", want: 0, wantSet: false},
		{name: "object without field", raw: `{"scheduled_scan_enabled": true}`, want: 0, wantSet: false},
		{name: "blank string is unset", raw: `{"scan_interval": "  "}`, want: 0, wantSet: false},
		{name: "duration string", raw: `{"scan_interval": "12h"}`, want: 12 * time.Hour, wantSet: true},
		{name: "duration with surrounding space", raw: `{"scan_interval": " 90m "}`, want: 90 * time.Minute, wantSet: true},
		{name: "unparseable duration", raw: `{"scan_interval": "twelve hours"}`, wantErr: "scan_interval"},
		{name: "numeric value is rejected", raw: `{"scan_interval": 3600}`, wantErr: "scan_interval must be a duration string, got number"},
		// A present key with a null value is not "unset": a template that meant
		// 12h and emitted null would otherwise run on the global cadence with no
		// signal (Codex review on #6724).
		{name: "null value is rejected", raw: `{"scan_interval": null}`, wantErr: "scan_interval must be a duration string, got null"},
		{name: "boolean value is rejected", raw: `{"scan_interval": true}`, wantErr: "scan_interval must be a duration string, got boolean"},
		{name: "object value is rejected without echoing it", raw: `{"scan_interval": {"token": "do-not-echo"}}`, wantErr: "scan_interval must be a duration string, got object"},
		// Unrelated values stay opaque: json.Valid accepts 1e400 but a float64
		// decode of the whole document would not (Codex review on #6724).
		{name: "unrelated out-of-range number is ignored when unset", raw: `{"opaque": 1e400}`, want: 0, wantSet: false},
		{name: "unrelated out-of-range number is ignored when set", raw: `{"opaque": 1e400, "scan_interval": "1h"}`, want: time.Hour, wantSet: true},
		{name: "array value is rejected", raw: `{"scan_interval": ["1h"]}`, wantErr: "scan_interval must be a duration string, got array"},
		{name: "null document is unset", raw: `null`, want: 0, wantSet: false},
		// A valid non-object document carries no scan_interval field, so it is
		// unset: DesiredCollectorInstance.Validate accepts any valid JSON here and
		// a generic or disabled collector may legitimately carry [] (Codex review
		// on #6724).
		{name: "array document is unset", raw: `[]`, want: 0, wantSet: false},
		{name: "scalar document is unset", raw: `42`, want: 0, wantSet: false},
		{name: "string document is unset", raw: `"text"`, want: 0, wantSet: false},
		{name: "unclosed object", raw: `{"scan_interval": "1h"`, wantErr: "decode collector configuration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, set, err := ParseScanInterval(tc.raw)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseScanInterval(%q) error = %v, want containing %q", tc.raw, err, tc.wantErr)
				}
				if strings.Contains(err.Error(), "do-not-echo") {
					t.Fatalf("ParseScanInterval(%q) echoed the rejected value: %v", tc.raw, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseScanInterval(%q) error = %v, want nil", tc.raw, err)
			}
			if set != tc.wantSet || got != tc.want {
				t.Fatalf("ParseScanInterval(%q) = (%s, %v), want (%s, %v)", tc.raw, got, set, tc.want, tc.wantSet)
			}
		})
	}
}

// A present null arrives in a RawMessage map entry as the literal null, not
// as an empty value, and unmarshalling that literal into a string succeeds
// silently. These pins keep the type check from being mistaken for dead code.
func TestJSONTypeNameNamesEveryDecodedShape(t *testing.T) {
	t.Parallel()

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal([]byte(`{"scan_interval": null}`), &decoded); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	raw, present := decoded["scan_interval"]
	if !present || string(raw) != "null" {
		t.Fatalf("present=%v raw=%q, want present with the literal null", present, raw)
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil || text != "" {
		t.Fatalf("unmarshal null into string: err=%v text=%q, want silent success with empty text", err, text)
	}
	cases := map[string]string{
		`null`: "null", `""`: "string", `"1h"`: "string", `42`: "number", `-1.5e3`: "number",
		`true`: "boolean", `false`: "boolean", `[]`: "array", `{}`: "object", ``: "null", `  "x" `: "string",
	}
	for raw, want := range cases {
		if got := jsonTypeName(json.RawMessage(raw)); got != want {
			t.Fatalf("jsonTypeName(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestParseScanIntervalValueMatchesTheDocumentParser pins that the value
// parser the status reader uses on configuration->'scan_interval' agrees with
// the document parser the coordinator uses at startup, so both read one
// configured value the same way (#8803).
func TestParseScanIntervalValueMatchesTheDocumentParser(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`"12h"`, `" 90m "`, `"  "`, `""`, `"twelve hours"`, `3600`, `null`, `true`, `["1h"]`, `{"a":1}`} {
		docValue, docSet, docErr := ParseScanInterval(`{"scan_interval": ` + raw + `}`)
		value, set, err := ParseScanIntervalValue(json.RawMessage(raw))
		if value != docValue || set != docSet || (err == nil) != (docErr == nil) {
			t.Fatalf("ParseScanIntervalValue(%s) = (%s, %v, %v), document parser = (%s, %v, %v)", raw, value, set, err, docValue, docSet, docErr)
		}
	}
	if _, set, err := ParseScanIntervalValue(nil); set || err != nil {
		t.Fatalf("ParseScanIntervalValue(nil) = (set=%v, err=%v), want unset without error", set, err)
	}
}
