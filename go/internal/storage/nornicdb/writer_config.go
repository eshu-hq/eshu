// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nornicdb

import (
	"sort"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// WriterConfig captures NornicDB canonical statement row-shape tuning.
type WriterConfig struct {
	FileBatchSize            int
	EntityBatchSize          int
	EntityLabelBatchSizes    map[string]int
	BatchedEntityContainment bool
}

// DefaultWriterConfig returns the evidence-backed production writer defaults.
func DefaultWriterConfig() WriterConfig {
	return WriterConfig{
		FileBatchSize:            DefaultFileBatchSize,
		EntityBatchSize:          DefaultEntityBatchSize,
		EntityLabelBatchSizes:    DefaultEntityLabelBatchSizes(DefaultEntityBatchSize),
		BatchedEntityContainment: DefaultBatchedEntityContainment,
	}
}

// ConfigureCanonicalWriter applies NornicDB row and containment tuning. It
// also selects the bare path-conflict retirement shape, unconditionally:
// the attributed shape's OPTIONAL MATCH + WITH collect composition drops
// the id guard on NornicDB and DETACH DELETEs the live node in steady
// state, losing reducer edges no writer re-arms (#8173). Every production
// NornicDB canonical writer (projector, ingester) funnels through here;
// cmd/bootstrap-index mirrors the call in its own NornicDB branch.
func ConfigureCanonicalWriter(
	writer *sourcecypher.CanonicalNodeWriter,
	config WriterConfig,
) *sourcecypher.CanonicalNodeWriter {
	if writer == nil {
		return nil
	}
	// NornicDB cannot evaluate the #7245 infra search key clause, so the
	// NornicDB shape never carries it.
	writer = writer.WithoutInfraSearchKey().
		WithEntityContainmentInEntityUpsert().
		WithFileBatchSize(config.FileBatchSize).
		WithEntityBatchSize(config.EntityBatchSize).
		WithBareRepositoryRetirement()
	if config.BatchedEntityContainment {
		writer = writer.WithBatchedEntityContainmentInEntityUpsert()
	}
	labels := make([]string, 0, len(config.EntityLabelBatchSizes))
	for label := range config.EntityLabelBatchSizes {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	for _, label := range labels {
		writer = writer.WithEntityLabelBatchSize(label, config.EntityLabelBatchSizes[label])
	}
	return writer
}
