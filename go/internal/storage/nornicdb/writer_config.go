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

// ConfigureCanonicalWriter applies NornicDB row and containment tuning.
func ConfigureCanonicalWriter(
	writer *sourcecypher.CanonicalNodeWriter,
	config WriterConfig,
) *sourcecypher.CanonicalNodeWriter {
	if writer == nil {
		return nil
	}
	writer = writer.WithEntityContainmentInEntityUpsert().
		WithFileBatchSize(config.FileBatchSize).
		WithEntityBatchSize(config.EntityBatchSize)
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
