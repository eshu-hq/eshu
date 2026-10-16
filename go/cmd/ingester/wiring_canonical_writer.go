// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"github.com/eshu-hq/eshu/go/internal/graph"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// ingesterCanonicalWriterConfig captures backend-neutral canonical writer
// tuning after environment parsing has validated any backend-specific knobs.
type ingesterCanonicalWriterConfig struct {
	GraphBackend                     runtimecfg.GraphBackend
	FileBatchSize                    int
	EntityBatchSize                  int
	EntityLabelBatchSizes            map[string]int
	NornicDBBatchedEntityContainment bool
}

// configureIngesterCanonicalWriter applies the shared canonical writer shape
// used by official graph backends. Neo4j uses row-scoped batched containment to
// reduce statement count; NornicDB uses the same row-scoped containment after
// repo-scale proof showed file-scoped containment over-fragmented entity writes.
func configureIngesterCanonicalWriter(
	writer *sourcecypher.CanonicalNodeWriter,
	config ingesterCanonicalWriterConfig,
) *sourcecypher.CanonicalNodeWriter {
	if writer == nil {
		return nil
	}
	writer = writer.WithEntityContainmentInEntityUpsert()
	if config.EntityBatchSize > 0 {
		writer = writer.WithEntityBatchSize(config.EntityBatchSize)
	}
	if config.GraphBackend == runtimecfg.GraphBackendNeo4j {
		writer = writer.WithBatchedEntityContainmentInEntityUpsert()
	}
	if config.GraphBackend == runtimecfg.GraphBackendNornicDB {
		writer = storagenornicdb.ConfigureCanonicalWriter(writer, storagenornicdb.WriterConfig{
			FileBatchSize:            config.FileBatchSize,
			EntityBatchSize:          config.EntityBatchSize,
			EntityLabelBatchSizes:    config.EntityLabelBatchSizes,
			BatchedEntityContainment: config.NornicDBBatchedEntityContainment,
		})
	}
	return writer
}

// newIngesterCanonicalNodeWriter builds the canonical writer with the #7245
// infra search key dialect resolved from the configured graph backend: Neo4j
// writes the key, NornicDB keeps its statements byte for byte.
func newIngesterCanonicalNodeWriter(
	executor sourcecypher.Executor,
	graphBackend runtimecfg.GraphBackend,
	batchSize int,
	instruments *telemetry.Instruments,
) (*sourcecypher.CanonicalNodeWriter, error) {
	searchKey, err := search.DialectForBackend(graph.SchemaBackend(graphBackend))
	if err != nil {
		return nil, err
	}
	return sourcecypher.NewCanonicalNodeWriterWithDialect(executor, batchSize, instruments, searchKey), nil
}
