// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
)

// MultiScopeOptions configures one seeded synthetic-corpus generation run that
// produces MULTIPLE independent GCP project scopes in a single cassette
// (issue #4396 slice 6b). It exists so a caller that needs more than one work
// unit — for example the Ifá P3 determinism matrix, which found a single-scope
// cassette gives `ifa drive -workers N` exactly one work unit for ANY N,
// making the worker count inert — can generate a cassette with genuinely
// independent scopes for the driver to fan out across.
type MultiScopeOptions struct {
	// Seed is the deterministic PRNG seed passed unchanged to every per-scope
	// Generate call. The same Seed (with identical Scopes/ResourceCount) always
	// produces byte-identical output; see GenerateMultiScope's doc comment.
	Seed uint64
	// Scopes is the number K of independent GCP project scopes to generate.
	// Must be positive.
	Scopes int
	// ResourceCount is the number of gcp_cloud_resource facts generated per
	// scope, forwarded to each per-scope Options.ResourceCount. Must be
	// positive.
	ResourceCount int
	// CollectorLabel is the informational cassette.File.Collector value for
	// the merged cassette. Defaults to "gcp_synthetic" when empty, matching
	// Options.CollectorLabel's own default.
	CollectorLabel string
}

// validate returns a fail-closed error for a malformed MultiScopeOptions
// rather than generating a degenerate cassette with zero or negative scopes.
func (o MultiScopeOptions) validate() error {
	if o.Scopes <= 0 {
		return fmt.Errorf("synth/gcp: MultiScopeOptions.Scopes must be positive, got %d", o.Scopes)
	}
	if o.ResourceCount <= 0 {
		return fmt.Errorf("synth/gcp: MultiScopeOptions.ResourceCount must be positive, got %d", o.ResourceCount)
	}
	return nil
}

// multiScopeProjectPrefix names the deterministic synthetic GCP project ID
// family GenerateMultiScope assigns to each generated scope. It is
// intentionally distinct from DefaultDemoOrgProfile's own project identity
// scheme (demo_profile.go) and from the recorded demo-org cassette's
// "supply-chain-demo-project" so a multi-scope cassette can be replayed
// alongside either without any full_resource_name/uid collision.
const multiScopeProjectPrefix = "acme-demo-gcp-"

// scopeProjectID derives the deterministic, distinct ProjectID for the
// zero-indexed i-th scope of a multi-scope generation run, e.g.
// "acme-demo-gcp-00", "acme-demo-gcp-01", ... "acme-demo-gcp-07" for
// MultiScopeOptions.Scopes=8. The two-digit zero-padding keeps the ids
// lexicographically sortable up to 99 scopes, which is far beyond any
// realistic K for this generator.
func scopeProjectID(index int) string {
	return fmt.Sprintf("%s%02d", multiScopeProjectPrefix, index)
}

// GenerateMultiScope builds a deterministic, seeded synthetic GCP cassette
// containing opts.Scopes independent scopes and returns its canonical v1
// bytes. It calls the single-scope Generate exactly opts.Scopes times, once
// per scopeProjectID(i) for i in [0, opts.Scopes), with every other field of
// the per-scope Options held fixed (same Seed, same ResourceCount, same
// CollectorLabel) — so the same (Seed, Scopes, ResourceCount) input always
// derives the same K project ids in the same order and produces byte-identical
// merged output, proven by TestGenerateMultiScopeIsByteIdenticalForSameInputs.
//
// Disjointness is by construction, not by a runtime check: resources.go's
// buildOneResource embeds Options.ProjectID directly into every generated
// resource's full_resource_name
// ("//<host>/projects/<ProjectID>/<family>Name/<name>"), and the reducer's
// CloudResource node uid folds full_resource_name in
// (go/internal/reducer/gcp_resource_materialization.go's cloudResourceUID).
// Distinct ProjectIDs per scope therefore give K disjoint full_resource_name
// sets and K disjoint CloudResource node uids across the merged cassette — no
// two scopes' resources can MERGE onto the same graph node — proven by
// TestGenerateMultiScopeScopesHaveDisjointFullResourceNames. This is the
// correctness constraint the #4396 slice 6b architecture decision requires:
// without it, two scopes sharing a resource identity would legitimately race
// on last-writer-wins scope-derived properties (e.g. source_fact_id), making
// the determinism matrix red for a reason that is not a concurrency bug.
//
// Each per-scope Generate call already canonicalizes and fail-closed
// reloads its own single-scope output; GenerateMultiScope re-marshals the
// merged multi-scope cassette.File and re-runs replay.Canonicalize plus
// cassette.ParseAndValidate over the WHOLE merged file, mirroring Generate's
// own load-back guard for the combined result.
func GenerateMultiScope(opts MultiScopeOptions) ([]byte, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	collectorLabel := Options{CollectorLabel: opts.CollectorLabel}.collectorLabel()
	scopes := make([]cassette.Scope, 0, opts.Scopes)
	for i := 0; i < opts.Scopes; i++ {
		projectID := scopeProjectID(i)
		raw, err := Generate(Options{
			Seed:           opts.Seed,
			ProjectID:      projectID,
			ResourceCount:  opts.ResourceCount,
			CollectorLabel: opts.CollectorLabel,
		})
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: generate scope %d (project %s): %w", i, projectID, err)
		}
		file, err := cassette.ParseAndValidate(raw)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: parse generated scope %d (project %s): %w", i, projectID, err)
		}
		if len(file.Scopes) != 1 {
			return nil, fmt.Errorf("synth/gcp: scope %d (project %s): expected exactly one scope from Generate, got %d", i, projectID, len(file.Scopes))
		}
		scopes = append(scopes, file.Scopes[0])
	}

	merged := cassette.File{
		Collector:     collectorLabel,
		SchemaVersion: cassette.SchemaVersionV1,
		Scopes:        scopes,
	}

	rawMerged, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: marshal multi-scope cassette: %w", err)
	}
	canonical, err := canonicalizeValue(mustDecodeJSON(rawMerged))
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: canonicalize multi-scope cassette: %w", err)
	}
	if _, err := cassette.ParseAndValidate(canonical); err != nil {
		return nil, fmt.Errorf("synth/gcp: generated multi-scope cassette failed validation: %w", err)
	}
	return canonical, nil
}
