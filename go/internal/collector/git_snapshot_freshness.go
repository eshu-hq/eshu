// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"crypto/sha256"
	"fmt"
	"sort"
)

// snapshotFreshnessHint computes a deterministic hash from file digests and
// entity metadata. This replaces the old approach that JSON-marshaled the
// entire snapshot, including all file bodies, which created a massive
// temporary allocation for large repos.
func snapshotFreshnessHint(snapshot RepositorySnapshot) string {
	h := sha256.New()
	writeFreshnessHashf(h, "v2:file_count=%d\n", snapshot.FileCount)
	writeFreshnessHashf(h, "delta=%t\n", snapshot.Delta)
	for _, relativePath := range snapshot.DeltaRelativePaths {
		writeFreshnessHashf(h, "delta_path:%s\n", relativePath)
	}
	for _, relativePath := range snapshot.DeletedRelativePaths {
		writeFreshnessHashf(h, "deleted_path:%s\n", relativePath)
	}

	if len(snapshot.ContentFileMetas) > 0 {
		for _, meta := range snapshot.ContentFileMetas {
			writeFreshnessHashf(h, "file:%s:%s\n", meta.RelativePath, meta.Digest)
		}
	} else {
		for _, cf := range snapshot.ContentFiles {
			writeFreshnessHashf(h, "file:%s:%s\n", cf.RelativePath, cf.Digest)
		}
	}
	for _, meta := range snapshot.DocumentationFileMetas {
		writeFreshnessHashf(h, "doc:%s:%s\n", meta.RelativePath, meta.Digest)
	}

	writeFreshnessHashf(h, "entities=%d\n", len(snapshot.ContentEntities))
	for _, e := range snapshot.ContentEntities {
		writeFreshnessHashf(h, "entity:%s:%s:%d\n", e.RelativePath, e.EntityType, e.StartLine)
	}

	// Value-flow taint evidence (opt-in via ESHU_EMIT_DATAFLOW). Folded in only
	// when present so a gate-off snapshot keeps its existing hint (no churn),
	// while enabling the gate or a taint-only change yields a distinct hint and
	// is not skipped by the commit-time freshness comparison.
	if len(snapshot.TaintEvidence) > 0 {
		writeFreshnessHashf(h, "taint_evidence=%d\n", len(snapshot.TaintEvidence))
		for _, ev := range snapshot.TaintEvidence {
			writeFreshnessHashf(h, "taint:%s:%d:%d:%s:%s:%s\n",
				ev.FunctionUID, ev.SourceLine, ev.SinkLine, ev.SinkKind, ev.SourceKind, ev.Binding)
		}
	}
	if len(snapshot.InterprocTaintEvidence) > 0 {
		writeFreshnessHashf(h, "interproc_evidence=%d\n", len(snapshot.InterprocTaintEvidence))
		for _, ev := range snapshot.InterprocTaintEvidence {
			writeFreshnessHashf(h, "interproc:%s:%s:%s:%s\n",
				ev.SourceFunctionUID, ev.SinkFunctionUID, ev.SinkKind, ev.SourceKind)
		}
	}
	if len(snapshot.DataflowCatalogVersions) > 0 {
		writeFreshnessHashf(h, "dataflow_catalog_versions=%d\n", len(snapshot.DataflowCatalogVersions))
		for _, version := range snapshot.DataflowCatalogVersions {
			writeFreshnessHashf(h, "dataflow_catalog:%s:%s:%s\n",
				version.Language, version.Catalog, version.Version)
		}
	}
	// Function-summary uids (opt-in via ESHU_EMIT_DATAFLOW). Folded in only when
	// present so a gate-off snapshot keeps its existing hint (no churn), while the
	// commit that begins emitting graph_uid yields a distinct hint even for an
	// otherwise-unchanged repo — otherwise shouldSkipUnchangedGeneration would
	// drain the fact stream and the new graph_uid would never persist. Iterated in
	// the builder's deterministic order.
	if len(snapshot.FunctionSummaries) > 0 {
		writeFreshnessHashf(h, "function_summaries=%d\n", len(snapshot.FunctionSummaries))
		for _, s := range snapshot.FunctionSummaries {
			writeFreshnessHashf(h, "summary:%s:%s\n", s.FunctionID, s.GraphUID)
		}
	}

	for _, candidate := range snapshot.TerraformStateCandidates {
		writeFreshnessHashf(
			h, "tfstate_candidate:%s:%s:%d\n",
			candidate.RelativePath,
			candidate.PathHash,
			candidate.FileSize,
		)
	}

	importKeys := make([]string, 0, len(snapshot.ImportsMap))
	for k := range snapshot.ImportsMap {
		importKeys = append(importKeys, k)
	}
	sort.Strings(importKeys)
	for _, k := range importKeys {
		writeFreshnessHashf(h, "import:%s:", k)
		targets := snapshot.ImportsMap[k]
		sorted := make([]string, len(targets))
		copy(sorted, targets)
		sort.Strings(sorted)
		for _, v := range sorted {
			writeFreshnessHashf(h, "%s,", v)
		}
		_, _ = h.Write([]byte("\n"))
	}

	return fmt.Sprintf("snapshot:%x", h.Sum(nil))
}

func writeFreshnessHashf(h interface{ Write([]byte) (int, error) }, format string, args ...any) {
	_, _ = fmt.Fprintf(h, format, args...)
}
