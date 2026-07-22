// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	submodulev1 "github.com/eshu-hq/eshu/sdk/go/factschema/submodule/v1"
)

// Emit parses one ".gitmodules" file body and returns one "submodule.pin"
// fact envelope per declared submodule entry (Parse already drops sections
// missing a path or url). sourcePath is always ".gitmodules" (see
// IsGitmodulesPath) and is carried verbatim as both the payload's
// implicit source URI and the envelope's SourceRef.SourceURI/SourceRecordID.
//
// Each entry's URL is resolved to a canonical repo_id via ResolveRepoID;
// ResolvedRepoID is left nil whenever the URL is relative, ambiguous, or
// otherwise unresolvable — ResolveRepoID never guesses (see its doc
// comment). PinnedSHA is resolved per entry through
// ctx.PinnedSHAResolver (issue #5420 Phase 2b) when the caller sets one;
// it stays nil when no resolver is set or the resolver reports no gitlink
// for that path.
//
// Each envelope's payload is built from the typed submodulev1.Pin struct via
// factschema.EncodeSubmodulePin (Contract System v1 §3.1), never a
// hand-built map, so the emitted shape stays byte-identical to what
// factschema.DecodeSubmodulePin expects. An entry whose payload fails to
// encode is skipped rather than emitted with a malformed shape; this cannot
// happen in practice since every Pin field is a plain string or *string, but
// the error is never silently swallowed into a wrong fact.
func Emit(ctx FixtureContext, repoID, sourcePath, body string) []facts.Envelope {
	entries := Parse(body)
	if len(entries) == 0 {
		return nil
	}

	envelopes := make([]facts.Envelope, 0, len(entries))
	for _, entry := range entries {
		submoduleURL := entry.URL
		pin := submodulev1.Pin{
			CollectorInstanceID: ctx.CollectorInstanceID,
			ParentRepoID:        repoID,
			SubmodulePath:       entry.Path,
			SubmoduleURL:        &submoduleURL,
		}
		if ctx.PinnedSHAResolver != nil {
			pin.PinnedSHA = ctx.PinnedSHAResolver(entry.Path)
		}
		if resolvedRepoID := ResolveRepoID(entry.URL); resolvedRepoID != "" {
			pin.ResolvedRepoID = &resolvedRepoID
		}

		payload, err := factschema.EncodeSubmodulePin(pin)
		if err != nil {
			continue
		}
		stableKey := facts.StableID(facts.SubmodulePinFactKind, map[string]any{
			"parent_repo_id": repoID,
			"submodule_path": entry.Path,
		})
		envelopes = append(envelopes, newEnvelope(ctx, facts.SubmodulePinFactKind, stableKey, sourcePath, payload))
	}
	return envelopes
}
