// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

// gitGenerationContentVersion names the git collector and parser output
// contract (#7978). It is folded into every freshness hint, so a release that
// changes what the collector or parser emits for unchanged source yields a new
// hint and a new generation, while a release that does not change the output
// keeps every hint and stores nothing. Bump it in the same change that alters
// emitted facts for unchanged input without changing the parser registry
// bindings or the fact schema (a parser logic fix, a new fact field, a changed
// stable key): those two already move the version on their own (#7986).
// TestGenerationContentVersionPinsCollectorOutput enforces it: it pins a
// digest of the facts emitted for testdata/content_version to this value and
// fails when the output moves without a bump (ruling a9-117 d6). Do not derive
// it from the build version: that would store a duplicate generation of every
// repository on every release.
const gitGenerationContentVersion = "git-collector.v8"

// GenerationReconcileReasonUnspecified is the reconcile reason a generation
// carries when its snapshot was forced to reconcile without a decision reason,
// so the ingestion store still treats it as a reconciliation.
const GenerationReconcileReasonUnspecified = "unspecified"

// generationContentVersion caches GenerationContentVersion: the fact-schema
// registry and the parser registry are fixed for the life of the process.
var generationContentVersion = sync.OnceValue(func() string {
	registry := facts.SupportedSchemaVersions()
	kinds := make([]string, 0, len(registry))
	for kind := range registry {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	h := sha256.New()
	for _, kind := range kinds {
		writeFreshnessHashf(h, "%s=%s\n", kind, registry[kind])
	}
	return fmt.Sprintf("%s;parsers=%s;schema=%x",
		gitGenerationContentVersion, parser.DefaultRegistry().Fingerprint(), h.Sum(nil)[:8])
})

// GenerationContentVersion returns the version identity a git freshness hint
// folds in (#7978): the collector and parser output contract
// (gitGenerationContentVersion), a fingerprint of the parser registry, and a
// digest of the supported fact-schema registry. Two snapshots of the same
// content produce the same hint only when this identity is also the same.
//
// The parser fingerprint (#7986) covers every registered parser key, language,
// extension, and file name, so a parser-set change mints one new generation
// per git scope with no hand edit, even when the changed output is visible
// only on inputs outside testdata/content_version. The digest is deterministic
// across builds, so an unchanged parser set keeps every hint.
//
// The schema digest covers the whole registry, not only the fact kinds the git
// collector emits, so a schema bump of any kind makes every git scope store one
// new generation. That errs toward storing: a registry restricted to git kinds
// would turn a missed kind into a silent duplicate skip of new output. The
// schema version files changed in 13 commits in the 60 days before this change,
// an upper bound on such bumps.
func GenerationContentVersion() string {
	return generationContentVersion()
}

// snapshotFreshnessHint computes the v3 content identity of a snapshot for the
// running collector version. See snapshotFreshnessHintForVersion.
func snapshotFreshnessHint(snapshot RepositorySnapshot) string {
	return snapshotFreshnessHintForVersion(snapshot, GenerationContentVersion())
}

// snapshotFreshnessHintForVersion computes a deterministic hash from the
// content version, the default branch, file digests, and entity metadata. It
// does not fold the non-default git refs the repository fact carries
// (git_refs): no reader consumes them today (the repository fact builder is the
// only producer and nothing reads the key back), and pinned refs are their own
// scopes. A ref move with unchanged content therefore stores nothing. The
// ingestion store treats a snapshot whose hint and commit equal the active
// generation's as unchanged and stores nothing (#7978), so every input that
// changes emitted facts must reach this hash. It avoids JSON-marshaling the
// snapshot, which allocated every file body for large repos.
func snapshotFreshnessHintForVersion(snapshot RepositorySnapshot, contentVersion string) string {
	h := sha256.New()
	writeFreshnessHashf(h, "v3:content_version=%s\n", contentVersion)
	writeFreshnessHashf(h, "default_branch=%s\n", repositoryDefaultBranch(snapshot.GitRefs))
	writeFreshnessHashf(h, "file_count=%d\n", snapshot.FileCount)
	writeFreshnessHashf(h, "delta=%t\n", snapshot.Delta)
	if snapshot.Delta {
		// A delta is a diff from its baseline (#7319); the same head and paths
		// from another baseline is a different observation (ruling a9-120).
		writeFreshnessHashf(h, "delta_baseline=%s\n", snapshot.DeltaBaselineCommitSHA)
	}
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
	for _, meta := range snapshot.WorkflowImageFileMetas {
		writeFreshnessHashf(h, "workflow_image:%s:%s\n", meta.RelativePath, meta.Digest)
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
	// otherwise-unchanged repo — otherwise the ingestion store's unchanged-generation skip would
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
