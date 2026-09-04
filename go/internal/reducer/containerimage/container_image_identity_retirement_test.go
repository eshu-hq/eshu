// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package containerimage

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"

	"github.com/eshu-hq/eshu/go/internal/facts"
	ociregistryv1 "github.com/eshu-hq/eshu/sdk/go/factschema/ociregistry/v1"
)

const (
	retirementTestRepositoryID = "oci-registry://registry.example.com/team/api"
	retirementTestDigest       = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	retirementTestConfigDigest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func TestPlanContainerImageIdentityRetirementHonorsCompletenessSignals(t *testing.T) {
	t.Parallel()

	tagRef := "registry.example.com/team/api:prod"
	digestRef := "registry.example.com/team/api@" + retirementTestDigest
	tests := []struct {
		name           string
		decision       ContainerImageIdentityDecision
		evidence       []facts.Envelope
		warnings       []facts.Envelope
		wantLegacyIDs  int
		wantTombstones int
		wantHeld       map[string]int
	}{
		{
			name: "config blob unavailable maps config digest to manifest digest",
			decision: ContainerImageIdentityDecision{
				ImageRef:        digestRef,
				Digest:          retirementTestDigest,
				RepositoryID:    retirementTestRepositoryID,
				Outcome:         reducercontract.ContainerImageIdentityExactDigest,
				CanonicalWrites: 1,
			},
			evidence: []facts.Envelope{retirementManifestEnvelope()},
			warnings: []facts.Envelope{retirementWarningEnvelope(
				"config_blob_unavailable",
				retirementTestConfigDigest,
			)},
			wantHeld: map[string]int{containerImageIdentityRetireHoldConfigBlobUnavailable: 1},
		},
		{
			name: "truncated repository holds a tag ref",
			decision: ContainerImageIdentityDecision{
				ImageRef: tagRef,
				Outcome:  reducercontract.ContainerImageIdentityUnresolved,
			},
			warnings: []facts.Envelope{retirementWarningEnvelope("tag_list_truncated", "")},
			wantHeld: map[string]int{containerImageIdentityRetireHoldTagListTruncated: 1},
		},
		{
			name: "truncated repository does not hold a digest ref",
			decision: ContainerImageIdentityDecision{
				ImageRef: digestRef,
				Outcome:  reducercontract.ContainerImageIdentityUnresolved,
			},
			warnings:       []facts.Envelope{retirementWarningEnvelope("tag_list_truncated", "")},
			wantLegacyIDs:  1,
			wantTombstones: 1,
		},
		{
			name: "missing manifest digest holds repository tag refs",
			decision: ContainerImageIdentityDecision{
				ImageRef: tagRef,
				Outcome:  reducercontract.ContainerImageIdentityUnresolved,
			},
			warnings: []facts.Envelope{retirementWarningEnvelope("missing_manifest_digest", "")},
			wantHeld: map[string]int{containerImageIdentityRetireHoldMissingManifestDigest: 1},
		},
		{
			name: "missing manifest digest holds repository digest refs",
			decision: ContainerImageIdentityDecision{
				ImageRef: digestRef,
				Outcome:  reducercontract.ContainerImageIdentityUnresolved,
			},
			warnings: []facts.Envelope{retirementWarningEnvelope("missing_manifest_digest", "")},
			wantHeld: map[string]int{containerImageIdentityRetireHoldMissingManifestDigest: 1},
		},
		{
			name: "oversized config is deterministic and does not hold",
			decision: ContainerImageIdentityDecision{
				ImageRef: tagRef,
				Outcome:  reducercontract.ContainerImageIdentityUnresolved,
			},
			warnings:       []facts.Envelope{retirementWarningEnvelope("config_blob_oversized", retirementTestConfigDigest)},
			wantLegacyIDs:  1,
			wantTombstones: 1,
		},
		{
			name: "healthy demotion retires the prior tag decision",
			decision: ContainerImageIdentityDecision{
				ImageRef: tagRef,
				Outcome:  reducercontract.ContainerImageIdentityUnresolved,
			},
			wantLegacyIDs:  1,
			wantTombstones: 1,
		},
		{
			name: "canonical replay retires only the contradictory outcome",
			decision: ContainerImageIdentityDecision{
				ImageRef:        digestRef,
				Digest:          retirementTestDigest,
				RepositoryID:    retirementTestRepositoryID,
				Outcome:         reducercontract.ContainerImageIdentityExactDigest,
				CanonicalWrites: 1,
			},
			wantLegacyIDs: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			write := retirementTestWrite(tt.decision)
			plan, err := planContainerImageIdentityRetirement(write, tt.evidence, tt.warnings)
			if err != nil {
				t.Fatalf("planContainerImageIdentityRetirement() error = %v", err)
			}
			if got := len(plan.LegacyFactIDs); got != tt.wantLegacyIDs {
				t.Fatalf("legacy fact IDs = %v (len %d), want len %d", plan.LegacyFactIDs, got, tt.wantLegacyIDs)
			}
			if got := len(plan.Tombstones); got != tt.wantTombstones {
				t.Fatalf("tombstones = %v (len %d), want len %d", plan.Tombstones, got, tt.wantTombstones)
			}
			if !equalStringIntMaps(plan.HeldByReason, tt.wantHeld) {
				t.Fatalf("held by reason = %v, want %v", plan.HeldByReason, tt.wantHeld)
			}
			wantHeldDecisions := 0
			for _, count := range tt.wantHeld {
				wantHeldDecisions += count
			}
			if got := len(plan.HeldDecisions); got != wantHeldDecisions {
				t.Fatalf("held decisions = %v (len %d), want len %d", plan.HeldDecisions, got, wantHeldDecisions)
			}
			if tt.wantLegacyIDs == 0 {
				return
			}
			outcome, ok := containerImageIdentityLegacyOutcome(tt.decision)
			if !ok {
				t.Fatalf("legacy outcome missing for %q", tt.decision.ImageRef)
			}
			wantLegacyIDs := []string{legacyContainerImageIdentityFactID(
				write,
				containerImageIdentityDecisionWithOutcome(tt.decision, outcome),
			)}
			if !slices.Equal(plan.LegacyFactIDs, wantLegacyIDs) {
				t.Fatalf("legacy fact IDs = %v, want %v", plan.LegacyFactIDs, wantLegacyIDs)
			}
		})
	}
}

func TestPlanContainerImageIdentityRetirementNeverInventsUnevaluatedRefs(t *testing.T) {
	t.Parallel()

	evaluated := ContainerImageIdentityDecision{
		ImageRef: "registry.example.com/team/api:current",
		Outcome:  reducercontract.ContainerImageIdentityUnresolved,
	}
	write := retirementTestWrite(evaluated)
	plan, err := planContainerImageIdentityRetirement(write, nil, nil)
	if err != nil {
		t.Fatalf("planContainerImageIdentityRetirement() error = %v", err)
	}

	stale := evaluated
	stale.ImageRef = "registry.example.com/team/api:label-only"
	stale.Outcome = reducercontract.ContainerImageIdentityTagResolved
	stale.CanonicalWrites = 1
	staleFactID := legacyContainerImageIdentityFactID(write, stale)
	if slices.Contains(plan.LegacyFactIDs, staleFactID) {
		t.Fatalf("retirement plan contains unevaluated label-only fact %q", staleFactID)
	}
}

func TestContainerImageIdentityHandlerFailsClosedWhenRequiredWarningLoadFails(t *testing.T) {
	t.Parallel()

	loader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{
			gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
		},
		warningErr: errors.New("synthetic warning load failure"),
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader: loader,
		Writer:     writer,
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-5854-warning-load",
		Domain:       reducercontract.DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854",
		SourceSystem: "git",
		Cause:        "test",
	})
	if err == nil || !strings.Contains(err.Error(), "load active container image identity warnings") {
		t.Fatalf("Handle() error = %v, want required warning-load failure", err)
	}
	if got, want := loader.warningCalls, 1; got != want {
		t.Fatalf("warning loader calls = %d, want %d", got, want)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 after warning-load failure", writer.calls)
	}
}

func TestContainerImageIdentityHandlerFailsClosedOnMalformedRetirementWarning(t *testing.T) {
	t.Parallel()

	loader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{
			gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
		},
		warnings: []facts.Envelope{{
			FactID:   "warning-malformed",
			FactKind: facts.OCIRegistryWarningFactKind,
			Payload: map[string]any{
				"repository_id": retirementTestRepositoryID,
			},
		}},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader: loader,
		Writer:     writer,
		Now: func() time.Time {
			return time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC)
		},
	}

	_, err := handler.Handle(context.Background(), reducercontract.Intent{
		IntentID:     "intent-5854-malformed-warning",
		Domain:       reducercontract.DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854",
		SourceSystem: "git",
		Cause:        "test",
	})
	if err == nil {
		t.Fatal("Handle() error = nil, want malformed active warning to fail closed")
	}
	if !strings.Contains(err.Error(), "decode oci_registry.warning payload") {
		t.Fatalf("Handle() error = %q, want classified warning decode failure", err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 after malformed active warning", writer.calls)
	}
}

func TestContainerImageIdentityHandlerFailsClosedOnIncompleteSafetyWarningTargets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		warningCode  string
		repositoryID *string
		digest       *string
		wantField    string
	}{
		{
			name:        "tag truncation missing repository",
			warningCode: ociregistryv1.WarningCodeTagListTruncated,
			wantField:   "repository_id",
		},
		{
			name:         "tag truncation blank repository",
			warningCode:  ociregistryv1.WarningCodeTagListTruncated,
			repositoryID: stringPointer(" "),
			wantField:    "repository_id",
		},
		{
			name:         "tag truncation placeholder repository",
			warningCode:  ociregistryv1.WarningCodeTagListTruncated,
			repositoryID: stringPointer("oci-registry://warnings"),
			wantField:    "repository_id",
		},
		{
			name:        "missing manifest digest missing repository",
			warningCode: ociregistryv1.WarningCodeMissingManifestDigest,
			wantField:   "repository_id",
		},
		{
			name:         "missing manifest digest redacted repository",
			warningCode:  ociregistryv1.WarningCodeMissingManifestDigest,
			repositoryID: stringPointer("[redacted]"),
			wantField:    "repository_id",
		},
		{
			name:         "config blob warning missing digest",
			warningCode:  ociregistryv1.WarningCodeConfigBlobUnavailable,
			repositoryID: stringPointer(retirementTestRepositoryID),
			wantField:    "digest",
		},
		{
			name:         "config blob warning blank digest",
			warningCode:  ociregistryv1.WarningCodeConfigBlobUnavailable,
			repositoryID: stringPointer(retirementTestRepositoryID),
			digest:       stringPointer(" "),
			wantField:    "digest",
		},
		{
			name:         "config blob warning placeholder digest",
			warningCode:  ociregistryv1.WarningCodeConfigBlobUnavailable,
			repositoryID: stringPointer(retirementTestRepositoryID),
			digest:       stringPointer("sha256:placeholder"),
			wantField:    "digest",
		},
		{
			name:        "config blob warning missing repository",
			warningCode: ociregistryv1.WarningCodeConfigBlobUnavailable,
			digest:      stringPointer(retirementTestConfigDigest),
			wantField:   "repository_id",
		},
		{
			name:         "config blob warning placeholder repository",
			warningCode:  ociregistryv1.WarningCodeConfigBlobUnavailable,
			repositoryID: stringPointer("oci-registry://warnings"),
			digest:       stringPointer(retirementTestConfigDigest),
			wantField:    "repository_id",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := map[string]any{"warning_code": tt.warningCode}
			if tt.repositoryID != nil {
				payload["repository_id"] = *tt.repositoryID
			}
			if tt.digest != nil {
				payload["digest"] = *tt.digest
			}
			writer := &recordingContainerImageIdentityWriter{}
			handler := ContainerImageIdentityHandler{
				FactLoader: &stubContainerImageIdentityFactLoader{
					scopeFacts: []facts.Envelope{
						gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
					},
					warnings: []facts.Envelope{{
						FactID:   "warning-incomplete-target",
						FactKind: facts.OCIRegistryWarningFactKind,
						Payload:  payload,
					}},
				},
				Writer: writer,
			}

			_, err := handler.Handle(context.Background(), reducercontract.Intent{
				IntentID:     "intent-5854-incomplete-warning",
				Domain:       reducercontract.DomainContainerImageIdentity,
				ScopeID:      "repository:synthetic",
				GenerationID: "generation-5854",
				SourceSystem: "git",
				Cause:        "test",
			})
			if err == nil {
				t.Fatal("Handle() error = nil, want incomplete safety warning to fail closed")
			}
			if !strings.Contains(err.Error(), tt.wantField) {
				t.Fatalf("Handle() error = %q, want invalid %s classification", err, tt.wantField)
			}
			if writer.calls != 0 {
				t.Fatalf("writer calls = %d, want 0 after incomplete safety warning", writer.calls)
			}
		})
	}
}

func retirementTestWrite(decision ContainerImageIdentityDecision) ContainerImageIdentityWrite {
	return ContainerImageIdentityWrite{
		IntentID:     "intent-5854",
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854",
		SourceSystem: "git",
		Cause:        "test",
		EvidenceAsOf: time.Date(2026, time.July, 29, 12, 0, 0, 0, time.UTC),
		Decisions:    []ContainerImageIdentityDecision{decision},
	}
}

func retirementManifestEnvelope() facts.Envelope {
	return facts.Envelope{
		FactID:   "manifest-5854",
		FactKind: facts.OCIImageManifestFactKind,
		Payload: map[string]any{
			"repository_id": retirementTestRepositoryID,
			"digest":        retirementTestDigest,
			"config": map[string]any{
				"digest": retirementTestConfigDigest,
			},
		},
	}
}

func retirementWarningEnvelope(code string, digest string) facts.Envelope {
	return facts.Envelope{
		FactID:   "warning-" + code,
		FactKind: facts.OCIRegistryWarningFactKind,
		Payload: map[string]any{
			"repository_id": retirementTestRepositoryID,
			"warning_code":  code,
			"digest":        digest,
		},
	}
}

func stringPointer(value string) *string {
	return &value
}

func equalStringIntMaps(left map[string]int, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}
	return true
}
