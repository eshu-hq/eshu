// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/graph"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
)

const (
	provenanceReplayContainerUID  = "oci-descriptor://registry.example.invalid/replay/provenance@" + provenanceReplayContainerDigest
	provenanceReplayBaseUID       = "oci-descriptor://registry.example.invalid/replay/base@" + provenanceReplayBaseDigest
	provenanceReplayContainerRepo = "oci-registry://registry.example.invalid/replay/provenance"
	provenanceReplayBaseRepo      = "oci-registry://registry.example.invalid/replay/base"
)

type discardProvenanceReplayIntentWriter struct{}

func (discardProvenanceReplayIntentWriter) Enqueue(
	_ context.Context,
	intents []projector.ReducerIntent,
) (projector.IntentResult, error) {
	return projector.IntentResult{Count: len(intents)}, nil
}

func newProvenanceReplayProjectorRuntime(executor provenanceReplayExecutor) *projector.Runtime {
	phaseExecutor := storagenornicdb.PhaseGroupExecutor{
		Inner:                    executor,
		MaxStatements:            storagenornicdb.DefaultPhaseGroupStatements,
		DirectoryMaxStatements:   storagenornicdb.DefaultDirectoryPhaseStatements,
		FileMaxStatements:        storagenornicdb.DefaultFilePhaseStatements,
		EntityMaxStatements:      storagenornicdb.DefaultEntityPhaseStatements,
		EntityLabelMaxStatements: storagenornicdb.DefaultEntityLabelPhaseStatements(storagenornicdb.DefaultEntityPhaseStatements),
		EntityPhaseConcurrency:   storagenornicdb.DefaultEntityPhaseConcurrency(),
		DrainReader:              executor,
		RetractBatchSize:         storagenornicdb.DefaultCanonicalRetractBatchSize,
	}
	canonicalWriter := storagenornicdb.ConfigureCanonicalWriter(
		cypher.NewCanonicalNodeWriter(phaseExecutor, 500, nil),
		storagenornicdb.DefaultWriterConfig(),
	)
	return &projector.Runtime{
		CanonicalWriter: canonicalWriter,
		ContentWriter:   &content.MemoryWriter{},
		IntentWriter:    discardProvenanceReplayIntentWriter{},
	}
}

func projectProvenanceReplayCanonicalGeneration(
	ctx context.Context,
	t *testing.T,
	runtime *projector.Runtime,
	generation provenanceReplayGeneration,
) {
	t.Helper()
	if generation.generation.GenerationID == "replay-provenance-gen2" {
		generation.scope.PreviousGenerationExists = true
	}
	if _, err := runtime.Project(ctx, generation.scope, generation.generation, generation.facts); err != nil {
		t.Fatalf("project %s canonical nodes: %v", generation.generation.GenerationID, err)
	}
}

func assertProvenanceReplayCanonicalImages(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
	generationID string,
) {
	t.Helper()
	assertProvenanceReplayCanonicalImage(ctx, t, executor, provenanceReplayCanonicalImage{
		uid: provenanceReplayContainerUID, digest: provenanceReplayContainerDigest,
		repositoryID: provenanceReplayContainerRepo, generationID: generationID,
	})
	assertProvenanceReplayCanonicalImage(ctx, t, executor, provenanceReplayCanonicalImage{
		uid: provenanceReplayBaseUID, digest: provenanceReplayBaseDigest,
		repositoryID: provenanceReplayBaseRepo, generationID: generationID,
	})
}

type provenanceReplayCanonicalImage struct {
	uid          string
	digest       string
	repositoryID string
	generationID string
}

func assertProvenanceReplayCanonicalImage(
	ctx context.Context,
	t *testing.T,
	executor provenanceReplayExecutor,
	want provenanceReplayCanonicalImage,
) {
	t.Helper()
	rows, err := executor.readRows(ctx, `MATCH (node:OciImageManifest {uid: $uid})
RETURN labels(node) AS labels, node.uid AS uid, node.digest AS digest,
       node.repository_id AS repository_id, node.evidence_source AS evidence_source,
       node.scope_id AS scope_id, node.generation_id AS generation_id`, map[string]any{"uid": want.uid})
	if err != nil {
		t.Fatalf("read canonical image %q: %v", want.uid, err)
	}
	if len(rows) != 1 {
		t.Fatalf("canonical image %q rows = %#v, want one", want.uid, rows)
	}
	row := rows[0]
	assertProvenanceReplayLabels(t, row["labels"], []string{"ContainerImage", "OciImageManifest"})
	for key, expected := range map[string]any{
		"uid": want.uid, "digest": want.digest, "repository_id": want.repositoryID,
		"evidence_source": "projector/oci_registry", "scope_id": provenanceReplayScopeID,
		"generation_id": want.generationID,
	} {
		if actual := row[key]; actual != expected {
			t.Errorf("canonical image %q %s = %#v, want %#v", want.uid, key, actual, expected)
		}
	}
}

func assertProvenanceReplayLabels(t *testing.T, value any, want []string) {
	t.Helper()
	var got []string
	switch labels := value.(type) {
	case []any:
		got = make([]string, 0, len(labels))
		for _, label := range labels {
			got = append(got, fmt.Sprint(label))
		}
	case []string:
		got = append(got, labels...)
	default:
		t.Fatalf("canonical image labels have type %T, want list", value)
	}
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("canonical image labels = %v, want %v", got, want)
	}
}

func (e provenanceReplayExecutor) RunWrite(
	ctx context.Context,
	cypherText string,
	parameters map[string]any,
) (storagenornicdb.DrainWriteResult, error) {
	session := e.driver.NewSession(ctx, e.sessionConfig(neo4jdriver.AccessModeWrite))
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, cypherText, parameters)
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("execute drain write: %w", err)
	}
	rows := make([]map[string]any, 0)
	for result.Next(ctx) {
		record := result.Record()
		row := make(map[string]any, len(record.Keys))
		for _, key := range record.Keys {
			value, _ := record.Get(key)
			row[key] = value
		}
		rows = append(rows, row)
	}
	if err := result.Err(); err != nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("iterate drain write: %w", err)
	}
	summary, err := result.Consume(ctx)
	if err != nil {
		return storagenornicdb.DrainWriteResult{}, fmt.Errorf("consume drain write: %w", err)
	}
	return storagenornicdb.DrainWriteResult{
		Rows: rows, NodesDeleted: int64(summary.Counters().NodesDeleted()),
		RelationshipsDeleted: int64(summary.Counters().RelationshipsDeleted()),
	}, nil
}

func (e provenanceReplayExecutor) ExecuteCypher(ctx context.Context, stmt graph.CypherStatement) error {
	return e.Execute(ctx, cypher.Statement{Cypher: stmt.Cypher, Parameters: stmt.Parameters})
}
