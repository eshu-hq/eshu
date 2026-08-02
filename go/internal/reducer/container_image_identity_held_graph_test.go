// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type effectiveContainerImageIdentityWriter struct {
	result ContainerImageIdentityWriteResult
	err    error
}

func (effectiveContainerImageIdentityWriter) ContainerImageIdentityActivationEpoch(
	context.Context,
	string,
	string,
) (int64, error) {
	return 1, nil
}

func (w effectiveContainerImageIdentityWriter) WriteContainerImageIdentityDecisions(
	context.Context,
	ContainerImageIdentityWrite,
) (ContainerImageIdentityWriteResult, error) {
	return w.result, w.err
}

func TestContainerImageIdentityHandlerProjectsWarningHeldEffectiveSupports(t *testing.T) {
	t.Parallel()

	const (
		repositoryID = "repository:synthetic"
		childDigest  = "sha256:5740574057405740574057405740574057405740574057405740574057405740"
		baseDigest   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	loader := &stubContainerImageIdentityFactLoader{
		scopeFacts: []facts.Envelope{
			gitImageRefFact("git-tag-held", "registry.example.com/team/api:prod"),
		},
		warnings: []facts.Envelope{
			retirementWarningEnvelope("tag_list_truncated", ""),
		},
	}
	effective := []ContainerImageIdentityDecision{
		{
			ImageRef:                     "registry.example.com/team/api@" + childDigest,
			Digest:                       childDigest,
			RepositoryID:                 "oci-registry://registry.example.com/team/api",
			SourceRepositoryIDs:          []string{repositoryID},
			BuildProvenanceRepositoryIDs: []string{repositoryID},
			Outcome:                      ContainerImageIdentityExactDigest,
			CanonicalWrites:              1,
		},
		{
			ImageRef:                  "registry.example.com/team/base@" + baseDigest,
			Digest:                    baseDigest,
			RepositoryID:              "oci-registry://registry.example.com/team/base",
			BaseImageForRepositoryIDs: []string{repositoryID},
			Outcome:                   ContainerImageIdentityExactDigest,
			CanonicalWrites:           1,
		},
	}
	effectiveSupports := make([]containerImageIdentitySupport, 0, len(effective))
	for _, decision := range effective {
		support, err := containerImageIdentitySupportFromDecision(decision)
		if err != nil {
			t.Fatalf("normalize effective support: %v", err)
		}
		effectiveSupports = append(effectiveSupports, support)
	}
	identityWriter := effectiveContainerImageIdentityWriter{
		result: ContainerImageIdentityWriteResult{
			effectiveSupports:          effectiveSupports,
			effectiveProjectionPresent: true,
		},
	}
	builtFromWriter := &recordingContainerImageProvenanceEdgeWriter{}
	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:            loader,
		Writer:                identityWriter,
		ProvenanceEdgeWriter:  builtFromWriter,
		DerivedFromEdgeWriter: derivedFromWriter,
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-held-graph",
		Domain:       DomainContainerImageIdentity,
		ScopeID:      "git-repository-scope:" + repositoryID,
		GenerationID: "generation-held-graph",
		SourceSystem: "git",
		Cause:        "synthetic warning hold",
	})
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	if got := len(builtFromWriter.writeRows); got != 1 || len(builtFromWriter.writeRows[0]) != 1 {
		t.Fatalf("BUILT_FROM writes = %#v, want one retained exact edge", builtFromWriter.writeRows)
	}
	if got := len(derivedFromWriter.writeRows); got != 1 || len(derivedFromWriter.writeRows[0]) != 1 {
		t.Fatalf("DERIVED_FROM writes = %#v, want one retained exact edge", derivedFromWriter.writeRows)
	}
}

func TestContainerImageIdentityHandlerRejectsMissingEffectiveGraphProjection(t *testing.T) {
	t.Parallel()

	builtFromWriter := &recordingContainerImageProvenanceEdgeWriter{}
	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:            &stubContainerImageIdentityFactLoader{},
		Writer:                effectiveContainerImageIdentityWriter{},
		ProvenanceEdgeWriter:  builtFromWriter,
		DerivedFromEdgeWriter: derivedFromWriter,
	}
	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-missing-effective-projection",
		Domain:       DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-missing-effective-projection",
		SourceSystem: "git",
	})
	if err == nil || !strings.Contains(err.Error(), "omitted accepted effective graph projection") {
		t.Fatalf("Handle() error = %v, want missing effective projection error", err)
	}
	assertNoContainerImageIdentityGraphCalls(t, builtFromWriter, derivedFromWriter)
}

func TestContainerImageIdentityHandlerDoesNotProjectRejectedPublication(t *testing.T) {
	t.Parallel()

	builtFromWriter := &recordingContainerImageProvenanceEdgeWriter{}
	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader: &stubContainerImageIdentityFactLoader{},
		Writer: effectiveContainerImageIdentityWriter{
			err: errors.New("synthetic stale claim rejection"),
		},
		ProvenanceEdgeWriter:  builtFromWriter,
		DerivedFromEdgeWriter: derivedFromWriter,
	}
	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-rejected-effective-projection",
		Domain:       DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-rejected-effective-projection",
		SourceSystem: "git",
	})
	if err == nil || !strings.Contains(err.Error(), "synthetic stale claim rejection") {
		t.Fatalf("Handle() error = %v, want writer rejection", err)
	}
	assertNoContainerImageIdentityGraphCalls(t, builtFromWriter, derivedFromWriter)
}

func assertNoContainerImageIdentityGraphCalls(
	t *testing.T,
	builtFromWriter *recordingContainerImageProvenanceEdgeWriter,
	derivedFromWriter *recordingContainerImageDerivedFromEdgeWriter,
) {
	t.Helper()
	if len(builtFromWriter.retractCalls) != 0 || len(builtFromWriter.writeRows) != 0 {
		t.Fatalf("BUILT_FROM calls after rejected publication: retract=%v write=%v", builtFromWriter.retractCalls, builtFromWriter.writeRows)
	}
	if len(derivedFromWriter.retractCalls) != 0 || len(derivedFromWriter.writeRows) != 0 {
		t.Fatalf("DERIVED_FROM calls after rejected publication: retract=%v write=%v", derivedFromWriter.retractCalls, derivedFromWriter.writeRows)
	}
}
