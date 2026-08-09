// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

const (
	lambdaImageDigest     = "sha256:0000000000000000000000000000000000000000000000000000000000cc"
	lambdaImageRunID      = "5152"
	lambdaImageRepository = "repository:r_69256c06"
)

func TestSupplyChainDemoCICDCassetteCarriesLambdaImageBuildEvidence(t *testing.T) {
	t.Parallel()

	envelopes := replaySupplyChainDemoCICDCassette(t)
	var foundRuns, foundArtifacts int
	for _, envelope := range envelopes {
		schemaEnvelope := factschema.Envelope{
			FactKind: envelope.FactKind, SchemaVersion: envelope.SchemaVersion,
			StableFactKey: envelope.StableFactKey, ScopeID: envelope.ScopeID,
			GenerationID: envelope.GenerationID, CollectorKind: envelope.CollectorKind,
			SourceConfidence: envelope.SourceConfidence, ObservedAt: envelope.ObservedAt,
			IsTombstone: envelope.IsTombstone, Payload: envelope.Payload,
		}
		switch envelope.FactKind {
		case facts.CICDRunFactKind:
			run, err := factschema.DecodeCICDRun(schemaEnvelope)
			if err != nil {
				t.Fatalf("DecodeCICDRun(%q) error = %v", envelope.StableFactKey, err)
			}
			if run.RunID == lambdaImageRunID {
				foundRuns++
				if run.RepositoryID == nil || *run.RepositoryID != lambdaImageRepository ||
					envelope.GenerationID != "cassette-cicd-scd-gen1" || envelope.FencingToken != 1 {
					t.Fatalf("Lambda image run identity/lifecycle = %#v, generation %q, fencing %d", run, envelope.GenerationID, envelope.FencingToken)
				}
			}
		case facts.CICDArtifactFactKind:
			artifact, err := factschema.DecodeCICDArtifact(schemaEnvelope)
			if err != nil {
				t.Fatalf("DecodeCICDArtifact(%q) error = %v", envelope.StableFactKey, err)
			}
			if artifact.RunID == lambdaImageRunID {
				foundArtifacts++
				if artifact.ArtifactType == nil || *artifact.ArtifactType != "container_image" ||
					artifact.ArtifactDigest == nil || *artifact.ArtifactDigest != lambdaImageDigest ||
					envelope.GenerationID != "cassette-cicd-scd-gen2-artifact" || envelope.FencingToken != 2 {
					t.Fatalf("Lambda image artifact identity/lifecycle = %#v, generation %q, fencing %d", artifact, envelope.GenerationID, envelope.FencingToken)
				}
			}
		}
	}
	if foundRuns != 1 || foundArtifacts != 1 {
		t.Fatalf("Lambda image build evidence counts = (runs %d, artifacts %d), want (1, 1)", foundRuns, foundArtifacts)
	}
}

func TestSupplyChainDemoCICDCassetteProducesLambdaImageBuiltFromRow(t *testing.T) {
	t.Parallel()

	envelopes := replaySupplyChainDemoCICDCassette(t)
	envelopes = append(envelopes, replayCassette(t, "ociregistry", "supply-chain-demo.json")...)
	decisions := reducer.BuildContainerImageIdentityDecisions(envelopes)
	rows := reducer.ContainerImageBuiltFromRowsForReplayTest(decisions)
	want := map[string]any{"digest": lambdaImageDigest, "repository_id": lambdaImageRepository}
	matches := 0
	for _, row := range rows {
		if reflect.DeepEqual(row, want) {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("Lambda image BUILT_FROM row matches = %d, want 1; rows = %#v", matches, rows)
	}
}

func replaySupplyChainDemoCICDCassette(t *testing.T) []facts.Envelope {
	t.Helper()
	return replayCassette(t, "cicdrun", "supply-chain-demo.json")
}

func replayCassette(t *testing.T, pathElements ...string) []facts.Envelope {
	t.Helper()

	path := filepath.Join(append([]string{"..", "..", "..", "testdata", "cassettes"}, pathElements...)...)
	source, err := cassette.NewSource(path)
	if err != nil {
		t.Fatalf("cassette.NewSource(%q) error = %v", path, err)
	}

	var envelopes []facts.Envelope
	for {
		generation, ok, nextErr := source.Next(context.Background())
		if nextErr != nil {
			t.Fatalf("Source.Next() error = %v", nextErr)
		}
		if !ok {
			break
		}
		for envelope := range generation.Facts {
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes
}
