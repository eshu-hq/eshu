// SPDX-License-Identifier: BUSL-1.1

package ifa

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// LoadCassetteEnvelopes reads every fact envelope out of a committed cassette,
// in file order, with no family-specific knowledge.
//
// Exported because two tests in different packages need it -- one in this
// package spanning every family's cassette, one in materializededges -- and
// this package's AGENTS.md forbids duplicating a body: a second copy of a
// cassette decoder drifts from the original silently, which is the false-green
// class the guards exist to prevent. Callers that want a fatal-on-error helper
// wrap this; the wrapper is a few lines and carries no decoding logic.
func LoadCassetteEnvelopes(path string) ([]facts.Envelope, error) {
	src, err := cassette.NewSource(path)
	if err != nil {
		return nil, fmt.Errorf("cassette.NewSource(%s): %w", path, err)
	}
	var out []facts.Envelope
	for {
		gen, ok, err := src.Next(context.Background())
		if err != nil {
			return nil, fmt.Errorf("cassette next: %w", err)
		}
		if !ok {
			break
		}
		for env := range gen.Facts {
			out = append(out, env)
		}
	}
	return out, nil
}
