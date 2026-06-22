package telemetry

import "go.opentelemetry.io/otel/attribute"

// Metric dimension, bounded stage values, and span name for git-collector
// per-stage snapshot timing live in this sibling file so the frozen contract.go
// does not grow, mirroring contract_vaultlive.go and contract_cicd.go.
//
// The git-collector snapshot path runs several distinct stages
// (discovery, pre-scan, parse, materialization, value-flow evidence). Before
// this contract the only per-repository metric was the whole-snapshot
// eshu_dp_repo_snapshot_duration_seconds histogram, so an operator could see
// that a repository was slow but not which stage owned the cost. The stage
// dimension and span below make each stage independently graphable and
// trace-visible.
const (
	// MetricDimensionStage labels collector snapshot stage timing with a bounded
	// stage token from the SnapshotStage* set below. Producers MUST never use
	// repository paths, file paths, or other high-cardinality values here.
	MetricDimensionStage = "stage"
)

// Bounded stage label values for eshu_dp_collector_snapshot_stage_duration_seconds
// and the collector.snapshot_stage span. They name the snapshot phase, never any
// repository or file identity, so the metric stays low-cardinality and the trace
// stays leak-free. The set is closed: a new stage must be added here and to the
// telemetry contract test.
const (
	// SnapshotStageDiscovery is the repository file-set discovery and pruning
	// stage.
	SnapshotStageDiscovery = "discovery"
	// SnapshotStagePreScan is the import pre-scan stage that resolves the
	// cross-file import symbol map before parsing.
	SnapshotStagePreScan = "pre_scan"
	// SnapshotStageGoPackageSemanticPreScan is the Go package interface
	// pre-scan stage that resolves package semantic roots.
	SnapshotStageGoPackageSemanticPreScan = "go_package_semantic_prescan"
	// SnapshotStageParse is the per-file parse stage, including SCIP indexing
	// and per-file value-flow extraction.
	SnapshotStageParse = "parse"
	// SnapshotStageMaterialize is the content materialization stage that shapes
	// parsed files into content records and entities.
	SnapshotStageMaterialize = "materialize"
	// SnapshotStageValueFlowEvidence is the post-materialization stage that
	// builds taint evidence, interprocedural taint evidence, function summaries,
	// function sources, and dataflow catalog versions from the parsed files.
	SnapshotStageValueFlowEvidence = "value_flow_evidence"
)

// AttrStage returns a stage attribute for collector snapshot stage metrics and
// spans. Callers MUST pass a bounded SnapshotStage* value.
func AttrStage(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionStage, v)
}

// SpanCollectorSnapshotStage wraps one git-collector snapshot stage so a trace
// can attribute snapshot latency to discovery, pre-scan, parse, materialization,
// or value-flow evidence work.
const SpanCollectorSnapshotStage = "collector.snapshot_stage"

func init() {
	metricDimensionKeys = append(metricDimensionKeys, MetricDimensionStage)
	spanNames = append(spanNames, SpanCollectorSnapshotStage)
}
