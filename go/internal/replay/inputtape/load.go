// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inputtape

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
)

// LoadTape reads and validates an input tape from path. The path is operator- or
// test-supplied (a repo-shipped tape under testdata), not request-derived input.
func LoadTape(path string) (Tape, error) {
	// #nosec G304 -- path is an operator-supplied / repo-shipped tape location,
	// not user- or request-derived input.
	data, err := os.ReadFile(path)
	if err != nil {
		return Tape{}, fmt.Errorf("inputtape: read tape file %q: %w", path, err)
	}
	tape, err := ParseTape(data)
	if err != nil {
		return Tape{}, fmt.Errorf("inputtape: parse tape file %q: %w", path, err)
	}
	return tape, nil
}

// ParseTape decodes and validates a tape from its JSON bytes. It rejects trailing
// content after the document so a corrupted recording fails loudly.
func ParseTape(data []byte) (Tape, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var tape Tape
	if err := dec.Decode(&tape); err != nil {
		return Tape{}, fmt.Errorf("decode tape: %w", err)
	}
	// Require EOF after the first value. dec.More reports false outside an array
	// or object, so a second top-level value (appended or corrupted trailing
	// JSON) slips past More; decoding again and requiring io.EOF rejects it
	// instead of silently dropping the tail.
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Tape{}, fmt.Errorf("decode tape: unexpected trailing data after JSON document")
	}
	if err := tape.validate(); err != nil {
		return Tape{}, err
	}
	return tape, nil
}

// WriteTape serializes tape canonically and writes it to path with 0o644
// permissions, creating or truncating the file.
func WriteTape(path string, tape Tape) error {
	data, err := MarshalTape(tape)
	if err != nil {
		return fmt.Errorf("inputtape: marshal tape: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("inputtape: write tape file %q: %w", path, err)
	}
	return nil
}

// jsonMarshal marshals v to JSON bytes without HTML escaping, used as the
// intermediate form fed to the canonical serializer.
func jsonMarshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("inputtape: marshal: %w", err)
	}
	return buf.Bytes(), nil
}
