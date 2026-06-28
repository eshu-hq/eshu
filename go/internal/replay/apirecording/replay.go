// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apirecording

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
)

// Assert re-drives every recorded exchange against h with the same options and
// fails if any live response diverges from the recorded golden. It is the
// offline shape gate: a query-handler or envelope change that alters the
// response shape, status, or canonical body is caught here without a live
// backend. The returned error names the diverging exchange and shows the
// status/body diff so a reviewer sees exactly what shifted. A nil error means
// every recorded shape still holds.
func Assert(h http.Handler, recording Recording, opts Options) error {
	if h == nil {
		return errors.New("apirecording: handler is nil")
	}
	if err := recording.validate(); err != nil {
		return fmt.Errorf("apirecording: invalid recording: %w", err)
	}
	var mismatches []string
	for _, want := range recording.Exchanges {
		got, err := driveOne(h, want.Request, opts)
		if err != nil {
			return fmt.Errorf("apirecording: re-drive %q: %w", want.Request.Name, err)
		}
		if diff := diffResponse(want.Response, got); diff != "" {
			mismatches = append(mismatches, fmt.Sprintf("exchange %q (%s %s):\n%s",
				want.Request.Name, want.Request.Method, want.Request.Path, diff))
		}
	}
	if len(mismatches) > 0 {
		sort.Strings(mismatches)
		return fmt.Errorf("apirecording: %d recorded response shape(s) diverged:\n%s",
			len(mismatches), strings.Join(mismatches, "\n\n"))
	}
	return nil
}

// diffResponse returns an empty string when want and got are equal, or a
// human-readable status/body diff otherwise. Bodies are compared by their
// canonical JSON bytes so a key-order or formatting difference is not reported
// as a divergence; only a genuine shape/value change is.
func diffResponse(want, got RecordedResponse) string {
	var lines []string
	if want.Status != got.Status {
		lines = append(lines, fmt.Sprintf("  status: recorded=%d live=%d", want.Status, got.Status))
	}
	wantBody := marshalCanonical(want.Body)
	gotBody := marshalCanonical(got.Body)
	if wantBody != gotBody {
		lines = append(lines, "  body:")
		lines = append(lines, "    --- recorded")
		lines = append(lines, indentBlock(wantBody))
		lines = append(lines, "    +++ live")
		lines = append(lines, indentBlock(gotBody))
	}
	return strings.Join(lines, "\n")
}

// marshalCanonical renders a recorded body value to stable indented JSON for
// comparison and diff display. The value already passed through the canonical
// core at record/re-drive time, so a plain indented marshal here is stable.
func marshalCanonical(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("<unmarshalable body: %v>", err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// indentBlock prefixes every line of s with a fixed indent so a multi-line body
// reads as an indented block under its diff header.
func indentBlock(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "      " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// Marshal returns the recording as stable indented JSON with a trailing newline,
// suitable for writing to a golden file. Exchanges are sorted by request name so
// the bytes are deterministic regardless of recording order.
func Marshal(recording Recording) ([]byte, error) {
	if err := recording.validate(); err != nil {
		return nil, fmt.Errorf("apirecording: invalid recording: %w", err)
	}
	sortExchanges(recording.Exchanges)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(recording); err != nil {
		return nil, fmt.Errorf("apirecording: marshal recording: %w", err)
	}
	return buf.Bytes(), nil
}

// LoadFile reads and validates a recording golden file.
func LoadFile(path string) (Recording, error) {
	// #nosec G304 -- path is a repo-shipped testdata golden location supplied by
	// the test, not user- or request-derived input.
	data, err := os.ReadFile(path)
	if err != nil {
		return Recording{}, fmt.Errorf("apirecording: read recording %q: %w", path, err)
	}
	var recording Recording
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&recording); err != nil {
		return Recording{}, fmt.Errorf("apirecording: parse recording %q: %w", path, err)
	}
	if err := recording.validate(); err != nil {
		return Recording{}, fmt.Errorf("apirecording: invalid recording %q: %w", path, err)
	}
	return recording, nil
}

// WriteFile writes a recording to path as a stable golden file (the -update
// regeneration path). It validates and sorts before writing so a regenerated
// golden is deterministic.
func WriteFile(path string, recording Recording) error {
	data, err := Marshal(recording)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("apirecording: write recording %q: %w", path, err)
	}
	return nil
}
