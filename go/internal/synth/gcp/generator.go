// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// Options configures one seeded synthetic-corpus generation run.
type Options struct {
	// Seed is the deterministic PRNG seed. The same seed with the same
	// remaining options MUST produce a byte-identical cassette (issue #4581
	// acceptance criterion #1); Generate proves this via
	// TestGenerateIsByteIdenticalForSameSeed.
	Seed uint64
	// ProjectID is the synthetic GCP project id embedded in every generated
	// resource's full_resource_name and the scope identity. Required.
	ProjectID string
	// ResourceCount is the number of gcp_cloud_resource facts to generate.
	// Must be positive. Relationships, collection warnings, DNS records, and
	// IAM policy observations are derived from the generated resource set in
	// a fixed proportion, so the whole cassette scales with this one knob.
	ResourceCount int
	// CollectorLabel is the informational cassette.File.Collector value.
	// Defaults to "gcp_synthetic" when empty.
	CollectorLabel string
}

// validate returns a fail-closed error for a malformed Options rather than
// generating a degenerate or empty cassette.
func (o Options) validate() error {
	if strings.TrimSpace(o.ProjectID) == "" {
		return fmt.Errorf("synth/gcp: ProjectID is required")
	}
	if o.ResourceCount <= 0 {
		return fmt.Errorf("synth/gcp: ResourceCount must be positive, got %d", o.ResourceCount)
	}
	return nil
}

// collectorLabel returns the effective collector label.
func (o Options) collectorLabel() string {
	if strings.TrimSpace(o.CollectorLabel) != "" {
		return o.CollectorLabel
	}
	return "gcp_synthetic"
}

// Generate builds a deterministic, seeded synthetic GCP cassette and returns
// its canonical v1 bytes. The same Options.Seed (with identical remaining
// options) always yields byte-identical output — proven by
// TestGenerateIsByteIdenticalForSameSeed — because generation draws only from
// a math/rand/v2 PCG seeded from Options.Seed, iterates the fixed, sorted
// assetTypeInventory, and the output is passed through
// replay.Canonicalize(replay.DefaultCanonicalOptions()), the same fail-closed
// canonicalization go/internal/replay/recorder applies to a live-recorded
// cassette.
//
// Generate never touches the network or the filesystem, and no credential of
// any kind is read or required: every value is synthesized from the seed and
// Options.ProjectID. There is nothing to redact, satisfying the "no
// credentials, no network, no redaction step" acceptance criterion by
// construction.
func Generate(opts Options) ([]byte, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}

	// #nosec G404 -- a deterministic, seeded PRNG is the point: this generator
	// MUST produce byte-identical output for the same seed (issue #4581
	// acceptance criterion), which crypto/rand cannot do. No security decision
	// depends on this value; it only picks cosmetic variety (location, state)
	// for synthetic fixture data that carries nothing sensitive.
	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^seedMixConstant))
	gen := &generation{opts: opts, rng: rng}

	facts, err := gen.buildFacts()
	if err != nil {
		return nil, err
	}

	// The seed is part of the scope identity, not only cosmetic metadata: two
	// corpora for the same ProjectID but different Seed must be independently
	// replayable. Replay derives fact_id from
	// (scope_id, generation_id, stable_fact_key)
	// (go/internal/replay/cassette/source.go), generation_id derives from
	// scope_id (replay.DerivedGenerationID below), and the stable keys here are
	// project/resource-based (seed-independent). Folding the seed into scope_id
	// makes scope_id, generation_id, and every derived fact_id seed-distinct, so
	// replaying two seed-variants into one store keeps them independent instead
	// of the later run fencing/overwriting the earlier. Same-seed determinism is
	// unaffected: the seed is fixed per generation, so the scope_id is stable and
	// the byte-identical contract (TestGenerateIsByteIdenticalForSameSeed) holds.
	scopeID := fmt.Sprintf("gcp:project:%s:seed:%d", opts.ProjectID, opts.Seed)
	file := cassette.File{
		Collector:     opts.collectorLabel(),
		SchemaVersion: cassette.SchemaVersionV1,
		Scopes: []cassette.Scope{
			{
				ScopeID:       scopeID,
				SourceSystem:  "gcp",
				ScopeKind:     "account",
				CollectorKind: "gcp",
				PartitionKey:  scopeID,
				Metadata: map[string]string{
					"project_id": opts.ProjectID,
					"synthetic":  "true",
					"seed":       strconv.FormatUint(opts.Seed, 10),
				},
				GenerationID: replay.DerivedGenerationID(scopeID),
				ObservedAt:   time.Now().UTC(),
				TriggerKind:  "snapshot",
				Facts:        facts,
			},
		},
	}

	raw, err := json.Marshal(file)
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: marshal cassette: %w", err)
	}
	canonical, err := canonicalizeValue(mustDecodeJSON(raw))
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: canonicalize cassette: %w", err)
	}
	// Load the output back through the real fail-closed codec: the generator
	// must never emit a cassette the replay loader would reject, mirroring
	// the recorder precedent's load-back guard (go/internal/replay/recorder).
	if _, err := cassette.ParseAndValidate(canonical); err != nil {
		return nil, fmt.Errorf("synth/gcp: generated cassette failed validation: %w", err)
	}
	return canonical, nil
}

// seedMixConstant folds a fixed constant into the PCG's second stream
// parameter so two different Options.Seed values reliably select distinct
// PCG streams instead of only differing in the first parameter.
const seedMixConstant = 0x9E3779B97F4A7C15

// canonicalizeValue re-exports replay.CanonicalizeValue with this package's
// fixed cassette canonicalization options, so both Generate and the
// idempotence test share one canonicalization call site.
func canonicalizeValue(value any) ([]byte, error) {
	canonical, err := replay.CanonicalizeValue(value, replay.DefaultCanonicalOptions())
	if err != nil {
		return nil, fmt.Errorf("synth/gcp: canonicalize: %w", err)
	}
	return canonical, nil
}

// mustDecodeJSON decodes freshly-marshaled JSON this package produced. A
// decode error here would mean json.Marshal produced invalid JSON, which
// cannot happen for the types Generate marshals; panicking surfaces a
// programming error immediately rather than returning a confusing wrapped
// error from a call site that cannot actually fail in practice.
func mustDecodeJSON(raw []byte) any {
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		panic(fmt.Sprintf("synth/gcp: decode just-marshaled cassette JSON: %v", err))
	}
	return value
}

// generation holds the mutable state for one Generate call: the RNG stream
// and accumulated resources available for deriving relationships, warnings,
// DNS records, and IAM observations.
type generation struct {
	opts      Options
	rng       *rand.Rand
	resources []gcpv1.Resource
}

// buildFacts generates the full ordered fact set for one cassette scope:
// gcp_cloud_resource facts first (so later kinds can reference them), then
// derived gcp_cloud_relationship, gcp_collection_warning, gcp_dns_record, and
// gcp_iam_policy_observation facts in fixed proportion to the resource count.
func (g *generation) buildFacts() ([]cassette.Fact, error) {
	var out []cassette.Fact

	resourceFacts, err := g.buildResourceFacts()
	if err != nil {
		return nil, err
	}
	out = append(out, resourceFacts...)

	relationshipFacts, err := g.buildRelationshipFacts()
	if err != nil {
		return nil, err
	}
	out = append(out, relationshipFacts...)

	warningFacts, err := g.buildCollectionWarningFacts()
	if err != nil {
		return nil, err
	}
	out = append(out, warningFacts...)

	dnsFacts, err := g.buildDNSRecordFacts()
	if err != nil {
		return nil, err
	}
	out = append(out, dnsFacts...)

	iamFacts, err := g.buildIAMPolicyObservationFacts()
	if err != nil {
		return nil, err
	}
	out = append(out, iamFacts...)

	return out, nil
}

// generateFact builds one cassette.Fact for factKind from a schema-valid,
// typed payload. It fails closed (returns an error, emits nothing) when
// factKind has no entry in factKindSchemaVersions — the "generation FAILS
// CLOSED on a kind with no schema" acceptance criterion — rather than falling
// back to a hand-built map[string]any payload for an unrecognized kind.
func generateFact(factKind string, schemaVersion string, payload map[string]any) (cassette.Fact, error) {
	if _, ok := factKindSchemaVersions[factKind]; !ok {
		return cassette.Fact{}, fmt.Errorf("synth/gcp: fact kind %q has no registered #4567 schema; refusing to generate", factKind)
	}
	if payload == nil {
		return cassette.Fact{}, fmt.Errorf("synth/gcp: fact kind %q: nil payload", factKind)
	}
	return cassette.Fact{
		FactKind:         factKind,
		SchemaVersion:    schemaVersion,
		CollectorKind:    "gcp",
		FencingToken:     1,
		SourceConfidence: "observed",
		Payload:          payload,
	}, nil
}

// factKindSchemaVersions is the fail-closed allow-list of GCP fact kinds this
// generator may emit, each mapped to its #4567 payload schema version. A kind
// not listed here has no checked-in JSON Schema this generator can validate
// against, so generateFact refuses to emit it.
var factKindSchemaVersions = map[string]string{
	factschema.FactKindGCPCloudResource:        "1.1.0",
	factschema.FactKindGCPCloudRelationship:    "1.0.0",
	factschema.FactKindGCPCollectionWarning:    "1.0.0",
	factschema.FactKindGCPDNSRecord:            "1.0.0",
	factschema.FactKindGCPIAMPolicyObservation: "1.0.0",
}
