// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package schemadecode

import (
	"errors"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu-sdk/factschema"
	ociregistryv1 "github.com/eshu-hq/eshu-sdk/factschema/ociregistry/v1"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/factdecode"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// TestOCIRegistryWarningDecodeSeam covers the fail-closed OCI warning
// seam: a well-formed warning decodes, and a warning missing its code
// quarantines as input_invalid naming the field.
func TestOCIRegistryWarningDecodeSeam(t *testing.T) {
	t.Parallel()
	runDecodeCases(t, []decodeCase{
		{
			name: "OCIRegistryWarning", kind: "oci_registry.warning", target: &ociregistryv1.Warning{},
			decode:        func(env facts.Envelope) (any, error) { return DecodeOCIRegistryWarning(env) },
			requiredField: "warning_code",
			spotFields:    map[string]any{"warning_code": "stale_snapshot"},
		},
	})
}

// ociIndexCase describes one skip-on-malformed OCI registry-index decode:
// valid envelopes return ok=true with identity intact, malformed ones
// return ok=false without an error (the projector already dead-lettered
// the fact, so double-counting would lie about failure volume).
type ociIndexCase struct {
	name          string
	kind          string
	target        any
	decode        func(facts.Envelope) (any, bool)
	requiredField string
	spotField     string
	spotValue     string
}

func runOCIIndexCases(t *testing.T, cases []ociIndexCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name+"/Valid", func(t *testing.T) {
			t.Parallel()
			payload := populatedPayloadForType(reflect.TypeOf(tc.target).Elem())
			payload[tc.spotField] = tc.spotValue
			decoded, ok := tc.decode(decodeTestEnvelope(tc.kind, payload))
			if !ok {
				t.Fatalf("%s: ok = false, want true for a well-formed envelope", tc.kind)
			}
			requireRoundTrip(t, tc.kind, tc.spotField, decodedFieldByJSON(t, decoded, tc.spotField), tc.spotValue)
		})
		t.Run(tc.name+"/MalformedSkips", func(t *testing.T) {
			t.Parallel()
			payload := populatedPayloadForType(reflect.TypeOf(tc.target).Elem())
			delete(payload, tc.requiredField)
			_, ok := tc.decode(decodeTestEnvelope(tc.kind, payload))
			if ok {
				t.Fatalf("%s: ok = true, want false for an envelope missing %q", tc.kind, tc.requiredField)
			}
		})
	}
}

func TestOCIRegistryIndexSkips(t *testing.T) {
	t.Parallel()
	runOCIIndexCases(t, []ociIndexCase{
		{
			name: "ImageManifest", kind: "oci_registry.image_manifest", target: &ociregistryv1.ImageManifest{},
			decode: func(env facts.Envelope) (any, bool) {
				manifest, ok := DecodeOCIImageManifestForIndex(env)
				return manifest, ok
			},
			requiredField: "digest",
			spotField:     "digest", spotValue: "sha256:abc123",
		},
		{
			name: "ImageIndex", kind: "oci_registry.image_index", target: &ociregistryv1.ImageIndex{},
			decode: func(env facts.Envelope) (any, bool) {
				index, ok := DecodeOCIImageIndexForIndex(env)
				return index, ok
			},
			requiredField: "digest",
			spotField:     "digest", spotValue: "sha256:def456",
		},
		{
			name: "ImageTagObservation", kind: "oci_registry.image_tag_observation", target: &ociregistryv1.TagObservation{},
			decode: func(env facts.Envelope) (any, bool) {
				observation, ok := DecodeOCIImageTagObservationForIndex(env)
				return observation, ok
			},
			requiredField: "tag",
			spotField:     "tag", spotValue: "v1.2.3",
		},
	})
}

// quarantineCase drives one residual-fatal quarantine builder through a
// classified decode error (classification and field carry over), a
// classification-less decode error (default survives, field carries),
// and a foreign error (defaults survive untouched).
type quarantineCase struct {
	name  string
	build func(facts.Envelope, error) factdecode.QuarantinedFact
}

func runQuarantineCases(t *testing.T, cases []quarantineCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name+"/ClassifiedDecodeError", func(t *testing.T) {
			t.Parallel()
			env := decodeTestEnvelope("repository", map[string]any{})
			deleteMe := &factschema.DecodeError{FactKind: "repository", Field: "repo_id", Classification: "unsupported_major"}
			got := tc.build(env, deleteMe)
			if got.FactID != env.FactID || got.FactKind != env.FactKind {
				t.Fatalf("quarantine identity = (%q, %q), want (%q, %q)", got.FactID, got.FactKind, env.FactID, env.FactKind)
			}
			if got.Classification != "unsupported_major" {
				t.Fatalf("Classification = %q, want the decode error's own classification", got.Classification)
			}
			if got.Field != "repo_id" {
				t.Fatalf("Field = %q, want repo_id", got.Field)
			}
		})
		t.Run(tc.name+"/UnclassifiedDecodeError", func(t *testing.T) {
			t.Parallel()
			env := decodeTestEnvelope("repository", map[string]any{})
			got := tc.build(env, &factschema.DecodeError{FactKind: "repository", Field: "repo_id"})
			if got.Classification != factschema.ClassificationInputInvalid {
				t.Fatalf("Classification = %q, want the input_invalid default", got.Classification)
			}
			if got.Field != "repo_id" {
				t.Fatalf("Field = %q, want repo_id", got.Field)
			}
		})
		t.Run(tc.name+"/ForeignErrorKeepsDefaults", func(t *testing.T) {
			t.Parallel()
			env := decodeTestEnvelope("repository", map[string]any{})
			got := tc.build(env, errors.New("boom"))
			if got.Classification != factschema.ClassificationInputInvalid {
				t.Fatalf("Classification = %q, want the input_invalid default", got.Classification)
			}
			if got.Field != "" {
				t.Fatalf("Field = %q, want empty for a non-decode error", got.Field)
			}
		})
	}
}

func TestResidualQuarantineBuilders(t *testing.T) {
	t.Parallel()
	runQuarantineCases(t, []quarantineCase{
		{name: "Codegraph", build: CodegraphDecodeQuarantine},
		{name: "ServiceCatalog", build: ServiceCatalogDecodeQuarantine},
	})
}

// TestCodegraphQuarantineFromRealDecodeError proves the quarantine
// builder round-trips a genuine seam failure end to end: decoding a
// repository envelope without repo_id, then quarantining that exact
// error, preserves the input_invalid classification and names repo_id.
func TestCodegraphQuarantineFromRealDecodeError(t *testing.T) {
	t.Parallel()
	env := decodeTestEnvelope("repository", map[string]any{})
	_, err := DecodeCodegraphRepository(env)
	if err == nil {
		t.Fatal("DecodeCodegraphRepository() error = nil, want a decode failure for an empty payload")
	}
	got := CodegraphDecodeQuarantine(env, err)
	if got.Classification != factschema.ClassificationInputInvalid {
		t.Fatalf("Classification = %q, want input_invalid", got.Classification)
	}
	if got.Field != "repo_id" {
		t.Fatalf("Field = %q, want repo_id", got.Field)
	}
}

func repositoryEnvelope(repoID string, withSourceRun bool) facts.Envelope {
	payload := map[string]any{"repo_id": repoID}
	if withSourceRun {
		payload["source_run_id"] = "run-1"
	}
	return facts.Envelope{
		FactID:   "fact-repo-" + repoID,
		FactKind: "repository",
		Payload:  payload,
	}
}

// TestBuildProjectionContexts proves the context builder's admission
// shape: a repository fact with identity and a source run yields exactly
// one context carrying scope, acceptance, run, and generation ids, while
// non-repository facts, identity-less facts, and run-less facts are
// skipped rather than admitted with empty identities.
func TestBuildProjectionContexts(t *testing.T) {
	t.Parallel()
	t.Run("AdmitsCompleteRepository", func(t *testing.T) {
		t.Parallel()
		env := repositoryEnvelope("github.com/example/repo", true)
		env.ScopeID = "scope-1"
		got := BuildProjectionContexts([]facts.Envelope{env}, "gen-9")
		context, ok := got["github.com/example/repo"]
		if !ok {
			t.Fatalf("contexts = %v, want an entry for the repository", got)
		}
		want := sharedintent.ProjectionContext{
			ScopeID:          "scope-1",
			AcceptanceUnitID: "github.com/example/repo",
			SourceRunID:      "run-1",
			GenerationID:     "gen-9",
		}
		if context != want {
			t.Fatalf("context = %+v, want %+v", context, want)
		}
	})
	t.Run("SkipsNonRepositoryFact", func(t *testing.T) {
		t.Parallel()
		env := repositoryEnvelope("github.com/example/repo", true)
		env.FactKind = "file"
		if got := BuildProjectionContexts([]facts.Envelope{env}, "gen-9"); len(got) != 0 {
			t.Fatalf("contexts = %v, want empty for a non-repository fact", got)
		}
	})
	t.Run("SkipsIdentityLessRepository", func(t *testing.T) {
		t.Parallel()
		env := decodeTestEnvelope("repository", map[string]any{"source_run_id": "run-1"})
		if got := BuildProjectionContexts([]facts.Envelope{env}, "gen-9"); len(got) != 0 {
			t.Fatalf("contexts = %v, want empty for a repository without repo_id", got)
		}
	})
	t.Run("SkipsWhitespaceRepoID", func(t *testing.T) {
		t.Parallel()
		env := repositoryEnvelope("   ", true)
		if got := BuildProjectionContexts([]facts.Envelope{env}, "gen-9"); len(got) != 0 {
			t.Fatalf("contexts = %v, want empty for a whitespace repo_id", got)
		}
	})
	t.Run("SkipsRepositoryWithoutSourceRun", func(t *testing.T) {
		t.Parallel()
		env := repositoryEnvelope("github.com/example/repo", false)
		if got := BuildProjectionContexts([]facts.Envelope{env}, "gen-9"); len(got) != 0 {
			t.Fatalf("contexts = %v, want empty for a repository without source_run_id", got)
		}
	})
	t.Run("SkipsWhitespaceSourceRun", func(t *testing.T) {
		t.Parallel()
		env := repositoryEnvelope("github.com/example/repo", true)
		env.Payload["source_run_id"] = "  "
		if got := BuildProjectionContexts([]facts.Envelope{env}, "gen-9"); len(got) != 0 {
			t.Fatalf("contexts = %v, want empty for a whitespace source_run_id", got)
		}
	})
}

// TestFactschemaEnvelopeVersionNormalization pins the adapter's three
// version spellings: empty and the Postgres "0.0.0" sentinel both mean
// version-less and normalize to the current major, while a stamped
// version passes through untouched.
func TestFactschemaEnvelopeVersionNormalization(t *testing.T) {
	t.Parallel()
	base := decodeTestEnvelope("aws_resource", map[string]any{})
	for _, version := range []string{"", "0.0.0"} {
		env := base
		env.SchemaVersion = version
		if got := FactschemaEnvelope(env).SchemaVersion; got != "1.0.0" {
			t.Errorf("version %q normalizes to %q, want 1.0.0", version, got)
		}
	}
	env := base
	env.SchemaVersion = "1.2.3"
	if got := FactschemaEnvelope(env).SchemaVersion; got != "1.2.3" {
		t.Errorf("version 1.2.3 normalizes to %q, want it untouched", got)
	}
}
