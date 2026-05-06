package main

import (
	"sort"

	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// bootstrapCanonicalWriterConfig captures canonical writer tuning for the
// one-shot bootstrap path after backend-specific environment validation.
type bootstrapCanonicalWriterConfig struct {
	GraphBackend                      runtimecfg.GraphBackend
	FileBatchSize                     int
	EntityBatchSize                   int
	EntityLabelBatchSizes             map[string]int
	OrderedEntityLabelBatchSizeLabels []string
}

// configureBootstrapCanonicalWriter applies the shared canonical writer shape
// used by official graph backends during one-shot bootstrap indexing. Neo4j
// uses row-scoped batched containment to reduce statement count, while
// NornicDB keeps the file-scoped default proven by the full-corpus benchmark.
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
