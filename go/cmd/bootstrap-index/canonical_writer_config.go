// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"sort"

	"github.com/eshu-hq/eshu/go/internal/graph"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/infra/search"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// bootstrapCanonicalWriterConfig captures canonical writer tuning for the
// one-shot bootstrap path after backend-specific environment validation.
type bootstrapCanonicalWriterConfig struct {
	GraphBackend                      runtimecfg.GraphBackend
	FileBatchSize                     int
	EntityBatchSize                   int
	EntityLabelBatchSizes             map[string]int
	DisableNornicDBBatchedContainment bool
	OrderedEntityLabelBatchSizeLabels []string
}

// configureBootstrapCanonicalWriter applies the shared canonical writer shape
// used by official graph backends during one-shot bootstrap indexing. Neo4j
// uses row-scoped batched containment to reduce statement count. NornicDB uses
// that same row-scoped shape by default after repo-scale proof showed
// file-scoped containment over-fragmented high-cardinality entity labels; the
// NornicDB fallback toggle restores the older file-scoped shape for measured
// comparisons.
func configureBootstrapCanonicalWriter(
	writer *sourcecypher.CanonicalNodeWriter,
	config bootstrapCanonicalWriterConfig,
) *sourcecypher.CanonicalNodeWriter {
	if writer == nil {
		return nil
	}
	writer = writer.WithEntityContainmentInEntityUpsert()
	if config.GraphBackend == runtimecfg.GraphBackendNeo4j {
		writer = writer.WithBatchedEntityContainmentInEntityUpsert()
	}
	if config.GraphBackend == runtimecfg.GraphBackendNornicDB {
		// The attributed retirement shape drops its id guard on NornicDB
		// and deletes the live node in steady state (#8173): same bare
		// selection storagenornicdb.ConfigureCanonicalWriter applies for
		// the projector and ingester NornicDB paths.
		writer = writer.WithBareRepositoryRetirement()
		if !config.DisableNornicDBBatchedContainment {
			writer = writer.WithBatchedEntityContainmentInEntityUpsert()
		}
		if config.FileBatchSize > 0 {
			writer = writer.WithFileBatchSize(config.FileBatchSize)
		}
		if config.EntityBatchSize > 0 {
			writer = writer.WithEntityBatchSize(config.EntityBatchSize)
		}
		for _, label := range config.OrderedEntityLabelBatchSizeLabels {
			batchSize := config.EntityLabelBatchSizes[label]
			writer = writer.WithEntityLabelBatchSize(label, batchSize)
		}
	}
	return writer
}

// orderedBootstrapEntityBatchLabels returns deterministic label order for
// applying label-specific canonical writer tuning.
func orderedBootstrapEntityBatchLabels(labelBatchSizes map[string]int) []string {
	labels := make([]string, 0, len(labelBatchSizes))
	for label := range labelBatchSizes {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

// newBootstrapCanonicalNodeWriter builds the canonical writer with the #7245
// infra search key dialect resolved from the configured graph backend: Neo4j
// writes the key, NornicDB keeps its statements byte for byte.
func newBootstrapCanonicalNodeWriter(
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
