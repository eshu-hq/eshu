// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	storagenornicdb "github.com/eshu-hq/eshu/go/internal/storage/nornicdb"
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
