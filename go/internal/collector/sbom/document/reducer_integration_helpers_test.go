// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package document_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/sbom/document"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

type parserEnvelopes struct {
	envelope facts.Envelope
}

func cycloneDXParserAdapter(raw []byte, ctx document.FixtureContext) ([]parserEnvelopes, error) {
	envelopes, err := document.CycloneDXFixtureEnvelopes(raw, ctx)
	if err != nil {
		return nil, err
	}
	return wrap(envelopes), nil
}

func spdxParserAdapter(raw []byte, ctx document.FixtureContext) ([]parserEnvelopes, error) {
	envelopes, err := document.SPDXFixtureEnvelopes(raw, ctx)
	if err != nil {
		return nil, err
	}
	return wrap(envelopes), nil
}

func wrap(envelopes []facts.Envelope) []parserEnvelopes {
	out := make([]parserEnvelopes, len(envelopes))
	for i, e := range envelopes {
		out[i] = parserEnvelopes{envelope: e}
	}
	return out
}

func unwrap(adapter []parserEnvelopes) []facts.Envelope {
	out := make([]facts.Envelope, len(adapter))
	for i, a := range adapter {
		out[i] = a.envelope
	}
	return out
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read fixture %q: %v", path, err)
	}
	return raw
}
