// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codeowners

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codeownersv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codeowners/v1"
)

// Emit parses one resolved CODEOWNERS file body and returns one
// "codeowners.ownership" fact envelope per emitted rule (Parse already drops
// pattern-only lines, comments, blanks, and section headers). sourcePath is
// the winning CODEOWNERS location (see ResolveWinner) and is carried
// verbatim as both the payload's source_path and the envelope's
// SourceRef.SourceURI.
//
// Each envelope's payload is built from the typed codeownersv1.Ownership
// struct via factschema.EncodeCodeownersOwnership (Contract System v1 §3.1),
// never a hand-built map, so the emitted shape stays byte-identical to what
// DecodeCodeownersOwnership expects. A rule whose payload fails to encode is
// skipped rather than emitted with a malformed shape; this cannot happen in
// practice since every Ownership field is a plain string/slice/int, but the
// error is never silently swallowed into a wrong fact.
func Emit(ctx FixtureContext, repoID, sourcePath, body string) []facts.Envelope {
	rules := Parse(body)
	if len(rules) == 0 {
		return nil
	}

	envelopes := make([]facts.Envelope, 0, len(rules))
	for _, rule := range rules {
		ownership := codeownersv1.Ownership{
			CollectorInstanceID: ctx.CollectorInstanceID,
			RepoID:              repoID,
			SourcePath:          sourcePath,
			Pattern:             rule.Pattern,
			Owners:              rule.Owners,
			OrderIndex:          rule.OrderIndex,
		}
		payload, err := factschema.EncodeCodeownersOwnership(ownership)
		if err != nil {
			continue
		}
		stableKey := facts.StableID(facts.CodeownersOwnershipFactKind, map[string]any{
			"repo_id":     repoID,
			"source_path": sourcePath,
			"pattern":     rule.Pattern,
			"order_index": rule.OrderIndex,
		})
		envelopes = append(envelopes, newEnvelope(ctx, facts.CodeownersOwnershipFactKind, stableKey, sourcePath, payload))
	}
	return envelopes
}
