// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func BenchmarkBuildContainerImageIdentitySupportSet(b *testing.B) {
	current := make([]ContainerImageIdentityDecision, 1000)
	converged := make([]ContainerImageIdentityDecision, 0, 2000)
	prior := make([]ContainerImageIdentityPriorSupport, 1000)
	for index := range current {
		digest := fmt.Sprintf("sha256:%064x", index)
		current[index] = ContainerImageIdentityDecision{
			ImageRef:         fmt.Sprintf("registry.example.com/performance/current-%d@%s", index, digest),
			Digest:           digest,
			RepositoryID:     "repository:example",
			Outcome:          ContainerImageIdentityExactDigest,
			CanonicalWrites:  1,
			IdentityStrength: "immutable_digest",
		}
		prior[index] = ContainerImageIdentityPriorSupport{
			ImageRef:         fmt.Sprintf("registry.example.com/performance/held-%d@%s", index, digest),
			Digest:           digest,
			RepositoryID:     "repository:example",
			Outcome:          string(ContainerImageIdentityExactDigest),
			CanonicalWrites:  1,
			IdentityStrength: "immutable_digest",
			SourceLayers:     []string{"observed_resource", "source_declaration"},
		}
		runtime := current[index]
		runtime.IdentityStrength = "explicit_digest"
		runtime.EvidenceFactIDs = []string{
			fmt.Sprintf("aws-image-reference:%d", index),
			fmt.Sprintf("oci-manifest:%d", index),
		}
		artifact := runtime
		artifact.IdentityStrength = "artifact_digest_with_registry_observation"
		artifact.EvidenceFactIDs = []string{
			fmt.Sprintf("ci-artifact:%d", index),
			fmt.Sprintf("ci-run:%d", index),
			fmt.Sprintf("oci-manifest:%d", index),
		}
		converged = append(converged, runtime, artifact)
	}
	for _, benchmark := range []struct {
		name      string
		decisions []ContainerImageIdentityDecision
		prior     []ContainerImageIdentityPriorSupport
		want      int
	}{
		{name: "current_1000", decisions: current, want: 1000},
		{name: "converged_1000_2000", decisions: converged, want: 2000},
		{name: "current_1000_held_1000", decisions: current, prior: prior, want: 2000},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			b.ReportAllocs()
			write := ContainerImageIdentityWrite{
				ScopeID:   "repository:performance",
				Decisions: benchmark.decisions,
			}
			for range b.N {
				set, err := buildContainerImageIdentitySupportSet(write, benchmark.prior)
				if err != nil {
					b.Fatal(err)
				}
				if len(set.Supports) != benchmark.want {
					b.Fatalf("supports = %d", len(set.Supports))
				}
			}
		})
	}
}

func TestBuildContainerImageIdentitySupportSetIsGenerationIndependent(t *testing.T) {
	t.Parallel()

	write := ContainerImageIdentityWrite{
		ScopeID:      "repository:example",
		GenerationID: "generation-a",
		Decisions: []ContainerImageIdentityDecision{{
			ImageRef:            "registry.example.com/team/app@sha256:aaaaaaaa",
			Digest:              "sha256:aaaaaaaa",
			RepositoryID:        "repository:example",
			SourceRepositoryIDs: []string{"repository:example", "repository:example"},
			Outcome:             ContainerImageIdentityExactDigest,
			CanonicalWrites:     1,
		}},
	}
	first, err := buildContainerImageIdentitySupportSet(write, nil)
	if err != nil {
		t.Fatalf("build support set: %v", err)
	}
	write.GenerationID = "generation-b"
	second, err := buildContainerImageIdentitySupportSet(write, nil)
	if err != nil {
		t.Fatalf("build repeated support set: %v", err)
	}
	if string(first.SetID) != string(second.SetID) || string(first.ContentHash) != string(second.ContentHash) {
		t.Fatal("unchanged scope support set changed across generations")
	}
	if len(first.Supports) != 1 || len(first.Supports[0].SourceRepositoryIDs) != 1 {
		t.Fatalf("normalized supports = %#v", first.Supports)
	}
}

func TestBuildContainerImageIdentitySupportSetRetainsAndDeduplicatesHeldSupport(t *testing.T) {
	t.Parallel()

	decision := ContainerImageIdentityDecision{
		ImageRef:            "registry.example.com/team/app@sha256:aaaaaaaa",
		Digest:              "sha256:aaaaaaaa",
		RepositoryID:        "repository:example",
		SourceRepositoryIDs: []string{"repository:example"},
		Outcome:             ContainerImageIdentityExactDigest,
		CanonicalWrites:     1,
	}
	prior := ContainerImageIdentityPriorSupport{
		Digest:              decision.Digest,
		ImageRef:            decision.ImageRef,
		RepositoryID:        decision.RepositoryID,
		Outcome:             string(decision.Outcome),
		CanonicalWrites:     decision.CanonicalWrites,
		SourceRepositoryIDs: []string{"repository:example", "repository:example"},
		SourceLayers:        []string{"source_declaration", "observed_resource"},
	}
	set, err := buildContainerImageIdentitySupportSet(ContainerImageIdentityWrite{
		ScopeID:   "repository:example",
		Decisions: []ContainerImageIdentityDecision{decision},
	}, []ContainerImageIdentityPriorSupport{prior, prior})
	if err != nil {
		t.Fatalf("build support set with held support: %v", err)
	}
	if len(set.Supports) != 1 {
		t.Fatalf("supports = %#v, want one semantic support", set.Supports)
	}
	if set.CurrentSupportCount != 1 {
		t.Fatalf("current support count = %d, want 1", set.CurrentSupportCount)
	}
}

func TestBuildContainerImageIdentitySupportSetPreservesHeldBaseAttribution(t *testing.T) {
	t.Parallel()

	const repositoryID = "repository:synthetic"
	childDigest := "sha256:" + strings.Repeat("57", 32)
	baseDigest := "sha256:" + strings.Repeat("40", 32)
	baseRef := "registry.example.com/team/base@" + baseDigest
	unrelatedRef := "registry.example.com/team/unrelated@sha256:" + strings.Repeat("aa", 32)
	write := ContainerImageIdentityWrite{
		ScopeID: "repository:synthetic",
		HeldDecisions: []ContainerImageIdentityDecision{
			{
				ImageRef:                  baseRef,
				Digest:                    baseDigest,
				Outcome:                   ContainerImageIdentityExactDigest,
				BaseImageForRepositoryIDs: []string{repositoryID},
			},
			{
				ImageRef:                  " " + baseRef + " ",
				Digest:                    baseDigest,
				Outcome:                   ContainerImageIdentityExactDigest,
				BaseImageForRepositoryIDs: []string{repositoryID, repositoryID},
			},
		},
	}
	prior := []ContainerImageIdentityPriorSupport{
		{
			Digest:                       childDigest,
			ImageRef:                     "registry.example.com/team/app@" + childDigest,
			Outcome:                      string(ContainerImageIdentityExactDigest),
			CanonicalWrites:              1,
			BuildProvenanceRepositoryIDs: []string{repositoryID},
		},
		{
			Digest:          baseDigest,
			ImageRef:        baseRef,
			Outcome:         string(ContainerImageIdentityExactDigest),
			CanonicalWrites: 1,
		},
		{
			Digest:          "sha256:" + strings.Repeat("aa", 32),
			ImageRef:        unrelatedRef,
			Outcome:         string(ContainerImageIdentityExactDigest),
			CanonicalWrites: 1,
		},
	}
	set, err := buildContainerImageIdentitySupportSet(write, prior)
	if err != nil {
		t.Fatalf("build held support set: %v", err)
	}
	for _, support := range set.Supports {
		switch support.ImageRef {
		case baseRef:
			if got := strings.Join(support.BaseImageForRepositoryIDs, ","); got != repositoryID {
				t.Fatalf("held base attribution = %q, want %q", got, repositoryID)
			}
		case unrelatedRef:
			if len(support.BaseImageForRepositoryIDs) != 0 {
				t.Fatalf("unrelated support attribution = %v, want empty", support.BaseImageForRepositoryIDs)
			}
		}
	}
	rows := containerImageDerivedFromSupportRows(set.Supports, repositoryID)
	if len(rows) != 1 || rows[0]["digest"] != childDigest || rows[0]["base_digest"] != baseDigest {
		t.Fatalf("held DERIVED_FROM rows = %#v, want child %q -> base %q", rows, childDigest, baseDigest)
	}
}

func TestSupportWriterLoadsPriorSupportOnlyForHeldReferences(t *testing.T) {
	t.Parallel()

	db := &recordingContainerImageIdentitySupportDB{claimValid: true}
	loader := &recordingContainerImageIdentityHeldSupportLoader{supports: []ContainerImageIdentityPriorSupport{{
		Digest:       "sha256:bbbbbbbb",
		ImageRef:     "registry.example.com/team/held@sha256:bbbbbbbb",
		RepositoryID: "repository:example",
		Outcome:      string(ContainerImageIdentityExactDigest),
	}}}
	writer := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: loader,
		ClaimedExecer:     db,
	}
	write := ContainerImageIdentityWrite{
		IntentID:        "intent-held",
		ClaimEpoch:      8,
		ActivationEpoch: 41,
		ScopeID:         "repository:example",
		GenerationID:    "generation-a",
		SourceSystem:    "git",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0),
		HeldDecisions: []ContainerImageIdentityDecision{{
			ImageRef: " registry.example.com/team/held@sha256:bbbbbbbb ",
		}, {
			ImageRef: "registry.example.com/team/held@sha256:bbbbbbbb",
		}},
	}
	result, err := writer.WriteContainerImageIdentityDecisions(context.Background(), write)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions(): %v", err)
	}
	if loader.calls != 1 || len(loader.imageRefs) != 1 ||
		loader.imageRefs[0] != "registry.example.com/team/held@sha256:bbbbbbbb" {
		t.Fatalf("held loader calls=%d refs=%v, want one normalized exact ref", loader.calls, loader.imageRefs)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("canonical writes = %d, want current decisions only", result.CanonicalWrites)
	}
	if !result.effectiveProjectionPresent || len(result.effectiveSupports) != 1 {
		t.Fatalf("effective projection present/supports = %t/%d, want true/1", result.effectiveProjectionPresent, len(result.effectiveSupports))
	}
	if support := result.effectiveSupports[0]; support.Digest != "sha256:bbbbbbbb" ||
		support.ImageRef != "registry.example.com/team/held@sha256:bbbbbbbb" {
		t.Fatalf("effective held support = %#v, want retained exact support", support)
	}
	if !strings.Contains(db.supportJSON, `"sha256:bbbbbbbb"`) {
		t.Fatalf("published supports = %s, want retained prior support", db.supportJSON)
	}
}

func TestSupportWriterDoesNotLoadPriorSupportWithoutHolds(t *testing.T) {
	t.Parallel()

	db := &recordingContainerImageIdentitySupportDB{claimValid: true}
	loader := &recordingContainerImageIdentityHeldSupportLoader{}
	writer := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: loader,
		ClaimedExecer:     db,
	}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:        "intent-current",
		ClaimEpoch:      7,
		ActivationEpoch: 41,
		ScopeID:         "repository:example",
		GenerationID:    "generation-a",
		SourceSystem:    "git",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0),
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions(): %v", err)
	}
	if loader.calls != 0 {
		t.Fatalf("held loader calls = %d, want 0", loader.calls)
	}
}

func TestSupportWriterDoesNotInventMissingHeldSupport(t *testing.T) {
	t.Parallel()

	db := &recordingContainerImageIdentitySupportDB{claimValid: true}
	loader := &recordingContainerImageIdentityHeldSupportLoader{}
	writer := PostgresContainerImageIdentitySupportWriter{
		HeldSupportLoader: loader,
		ClaimedExecer:     db,
	}
	result, err := writer.WriteContainerImageIdentityDecisions(
		context.Background(),
		ContainerImageIdentityWrite{
			IntentID:        "intent-missing-held",
			ClaimEpoch:      7,
			ActivationEpoch: 41,
			ScopeID:         "repository:example",
			GenerationID:    "generation-a",
			SourceSystem:    "git",
			EvidenceAsOf:    time.Unix(1_700_000_000, 0),
			HeldDecisions: []ContainerImageIdentityDecision{{
				ImageRef: "registry.example.com/team/missing@sha256:cccccccc",
			}},
		},
	)
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions(): %v", err)
	}
	if loader.calls != 1 || db.supportJSON != "[]" {
		t.Fatalf("loader calls=%d supports=%s, want one lookup and empty publication", loader.calls, db.supportJSON)
	}
	if result.CanonicalWrites != 0 {
		t.Fatalf("canonical writes = %d, want 0", result.CanonicalWrites)
	}
}

func TestSupportWriterRequiresExactClaimGenerationAndActivationEpoch(t *testing.T) {
	t.Parallel()

	db := &recordingContainerImageIdentitySupportDB{claimValid: true, activationEpoch: 41}
	writer := PostgresContainerImageIdentitySupportWriter{ClaimedExecer: db, ActivationLookup: db}
	epoch, err := writer.ContainerImageIdentityActivationEpoch(
		context.Background(), "repository:example", "generation-a",
	)
	if err != nil || epoch != 41 {
		t.Fatalf("activation epoch = %d, err = %v", epoch, err)
	}
	_, err = writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:        "intent-a",
		ClaimEpoch:      7,
		ActivationEpoch: epoch,
		ScopeID:         "repository:example",
		GenerationID:    "generation-a",
		SourceSystem:    "git",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0),
		Decisions: []ContainerImageIdentityDecision{{
			ImageRef:        "registry.example.com/team/app@sha256:aaaaaaaa",
			Digest:          "sha256:aaaaaaaa",
			RepositoryID:    "repository:example",
			Outcome:         ContainerImageIdentityExactDigest,
			CanonicalWrites: 1,
		}},
	})
	if err != nil {
		t.Fatalf("WriteContainerImageIdentityDecisions(): %v", err)
	}
	for _, fragment := range []string{
		"container_image_identity_storage_cutover",
		"work_item.work_item_id = $6",
		"work_item.container_image_identity_claim_epoch = $7",
		"work_item.container_image_identity_v3_required",
		"work_item.container_image_identity_v3_authorized_status = work_item.status",
		"state.activation_epoch = $8",
		"jsonb_to_recordset($17::jsonb)",
		"UPDATE container_image_identity_scope_state",
	} {
		if !strings.Contains(db.execQuery, fragment) {
			t.Errorf("publication SQL missing %q", fragment)
		}
	}
	if strings.Contains(db.execQuery, "INSERT INTO fact_records") ||
		strings.Contains(db.execQuery, "UPDATE fact_records") {
		t.Fatalf("v3 writer shadows fact_records instead of only cleaning legacy rows:\n%s", db.execQuery)
	}
	if !strings.Contains(db.execQuery, "DELETE FROM fact_records AS fact") {
		t.Fatalf("v3 writer does not atomically clean the scope's legacy rows:\n%s", db.execQuery)
	}
}

func TestSupportWriterRejectsStalePublication(t *testing.T) {
	t.Parallel()

	db := &recordingContainerImageIdentitySupportDB{claimValid: false}
	writer := PostgresContainerImageIdentitySupportWriter{ClaimedExecer: db}
	_, err := writer.WriteContainerImageIdentityDecisions(context.Background(), ContainerImageIdentityWrite{
		IntentID:        "intent-a",
		ClaimEpoch:      7,
		ActivationEpoch: 40,
		ScopeID:         "repository:example",
		GenerationID:    "generation-a",
		SourceSystem:    "git",
		EvidenceAsOf:    time.Unix(1_700_000_000, 0),
	})
	if err != ErrContainerImageIdentityClaimRejected {
		t.Fatalf("error = %v, want ErrContainerImageIdentityClaimRejected", err)
	}
}

type recordingContainerImageIdentitySupportDB struct {
	execQuery       string
	claimValid      bool
	legacyDeleted   int
	supportJSON     string
	activationEpoch int64
}

func (db *recordingContainerImageIdentitySupportDB) ExecContainerImageIdentityClaimed(
	_ context.Context,
	query string,
	args ...any,
) (int, bool, error) {
	db.execQuery = query
	if len(args) == 17 {
		db.supportJSON, _ = args[16].(string)
	}
	return db.legacyDeleted, db.claimValid, nil
}

func (db *recordingContainerImageIdentitySupportDB) ContainerImageIdentityActivationEpoch(
	context.Context,
	string,
	string,
) (int64, error) {
	return db.activationEpoch, nil
}

type recordingContainerImageIdentityHeldSupportLoader struct {
	calls     int
	imageRefs []string
	supports  []ContainerImageIdentityPriorSupport
}

func (loader *recordingContainerImageIdentityHeldSupportLoader) LoadHeldContainerImageIdentitySupports(
	_ context.Context,
	_ string,
	_ string,
	_ int64,
	imageRefs []string,
) ([]ContainerImageIdentityPriorSupport, error) {
	loader.calls++
	loader.imageRefs = append([]string(nil), imageRefs...)
	return append([]ContainerImageIdentityPriorSupport(nil), loader.supports...), nil
}
