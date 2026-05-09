package main

import (
	"context"
	"strings"
	"testing"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestConfigureIngesterCanonicalWriterBatchesContainmentAcrossFilesForNeo4j(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupChunkExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
	writer = configureIngesterCanonicalWriter(writer, ingesterCanonicalWriterConfig{
		GraphBackend: runtimecfg.GraphBackendNeo4j,
	})

	if err := writer.Write(context.Background(), canonicalWriterContainmentMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var batchedEntities int
	for _, stmt := range executor.groupStatements {
		if stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] == sourcecypher.CanonicalPhaseEntityContainment {
			t.Fatalf("Neo4j writer emitted separate entity_containment statement: %s", stmt.Cypher)
		}
		if stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] != sourcecypher.CanonicalPhaseEntities {
			continue
		}
		if strings.Contains(stmt.Cypher, "MATCH (f:File {path: row.file_path})") &&
			strings.Contains(stmt.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
			batchedEntities++
		}
	}
	if batchedEntities != 1 {
		t.Fatalf("batched entity containment statements = %d, want 1", batchedEntities)
	}
}

func TestCanonicalWriterContainmentMaterializationIncludesFileDirectories(t *testing.T) {
	t.Parallel()

	materialization := canonicalWriterContainmentMaterialization()
	directoriesByPath := make(map[string]struct{}, len(materialization.Directories))
	for _, directory := range materialization.Directories {
		directoriesByPath[directory.Path] = struct{}{}
	}

	for _, file := range materialization.Files {
		if file.DirPath == "" {
			continue
		}
		if _, ok := directoriesByPath[file.DirPath]; !ok {
			t.Fatalf("file %q references missing directory %q", file.Path, file.DirPath)
		}
	}
}

func TestConfigureIngesterCanonicalWriterBatchesContainmentAcrossFilesForNornicDBByDefault(t *testing.T) {
	t.Parallel()

	executor := &recordingGroupChunkExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
	writer = configureIngesterCanonicalWriter(writer, ingesterCanonicalWriterConfig{
		GraphBackend:                     runtimecfg.GraphBackendNornicDB,
		NornicDBBatchedEntityContainment: true,
	})

	if err := writer.Write(context.Background(), canonicalWriterContainmentMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var batchedEntities int
	for _, stmt := range executor.groupStatements {
		if stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] != sourcecypher.CanonicalPhaseEntities {
			continue
		}
		if strings.Contains(stmt.Cypher, "MATCH (f:File {path: row.file_path})") &&
			strings.Contains(stmt.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
			batchedEntities++
		}
	}
	if batchedEntities != 1 {
		t.Fatalf("NornicDB batched containment statements = %d, want 1", batchedEntities)
	}
}
