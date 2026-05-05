package main

import (
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ingesterCanonicalWriterConfig captures backend-neutral canonical writer
// tuning after environment parsing has validated any backend-specific knobs.
type ingesterCanonicalWriterConfig struct {
	GraphBackend                      runtimecfg.GraphBackend
	FileBatchSize                     int
	EntityBatchSize                   int
	EntityLabelBatchSizes             map[string]int
	NornicDBBatchedEntityContainment  bool
	OrderedEntityLabelBatchSizeLabels []string
}

// configureIngesterCanonicalWriter applies the shared canonical writer shape
// used by official graph backends. Neo4j uses row-scoped batched containment to
// reduce statement count; NornicDB stays on the benchmarked file-scoped default
// unless the latest-main batched containment knob is enabled explicitly.
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
		if config.FileBatchSize > 0 {
			writer = writer.WithFileBatchSize(config.FileBatchSize)
		}
		if config.NornicDBBatchedEntityContainment {
			writer = writer.WithBatchedEntityContainmentInEntityUpsert()
		}
		for _, label := range config.OrderedEntityLabelBatchSizeLabels {
			batchSize := config.EntityLabelBatchSizes[label]
			writer = writer.WithEntityLabelBatchSize(label, batchSize)
		}
	}
	return writer
}
