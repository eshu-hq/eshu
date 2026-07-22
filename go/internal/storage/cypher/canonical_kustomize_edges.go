// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// KustomizeOverlayRow is one (uid, path, base_refs) row for a KustomizeOverlay
// node, either freshly touched this materialization cycle (base_refs decoded
// from the parser's kustomize_overlays bucket) or read back from the graph by
// a KustomizeOverlayResolver (base_refs as persisted by a prior cycle).
type KustomizeOverlayRow struct {
	// UID is the KustomizeOverlay node's canonical uid (content.EntityRecord's
	// EntityID for this label).
	UID string
	// Path is the kustomization.yaml file's repo-relative path.
	Path string
	// BaseRefs is the overlay's local (non-remote) base directory references,
	// already normalized and filtered by the producer
	// (go/internal/parser/yaml/kustomize_semantics.go collectKustomizeBaseRefs).
	BaseRefs []string
}

// KustomizeOverlayResolver batch-reads every KustomizeOverlay node's (uid,
// path, base_refs) for one repository, mirroring the #5443
// TerraformStateConfigMatchResolver port
// (canonical_node_writer_tfstate_resolvers.go /
// tfstate_state_match_edge.go): defined here as a narrow port so this package
// depends on an interface, not a graph driver, and every cmd/* canonical-writer
// wiring site (cmd/ingester, cmd/bootstrap-index) adapts a read session to it.
// cmd/projector is deliberately NOT wired: mirroring
// go/cmd/ingester/terraform_state_ownership.go's own precedent
// ("cmd/projector exists only for [...] no Helm template deploys
// cmd/projector"), it is a non-deployed binary, so an unwired resolver there
// is safe (WithKustomizeOverlayResolver's nil default already fails closed,
// see its doc comment) rather than a production gap.
//
// This full-repo read exists because of a DeltaProjection accuracy hole a
// per-touched-file read cannot close: under a delta cycle a materialization
// carries entities only for the files that changed, so an untouched sibling
// overlay's uid (and its own base_refs) is not derivable in Go the way a
// same-cycle sibling like GitLab's job-to-job NEEDS resolution is
// (canonical_gitlab_edges.go) -- there is no line-number hash that reproduces
// an absent node's uid. Reading the full persisted set every cycle that
// touches or delta-deletes any KustomizeOverlay is what makes the #5445
// EXTENDS_BASE edge rebuild correct across deltas: an untouched overlay's
// persisted base_refs cannot be stale, because its own file did not change.
type KustomizeOverlayResolver interface {
	// ListKustomizeOverlays returns every KustomizeOverlay row for repoID, or
	// an error if the read fails. Callers MUST fail closed on a non-nil
	// error: skip the edge rebuild this cycle rather than write a partial or
	// wrong edge set.
	ListKustomizeOverlays(ctx context.Context, repoID string) ([]KustomizeOverlayRow, error)
}

// canonicalKustomizeExtendsBaseEdgeCypher writes the #5445 EXTENDS_BASE edge
// from a Kustomize overlay to the local base it declares. Both endpoints are
// matched by canonical key (uid), resolved in Go from the merged
// persisted+touched overlay set, not bound-variable properties -- mirroring
// canonicalNodeGitlabNeedsEdgeCypher.
const canonicalKustomizeExtendsBaseEdgeCypher = `UNWIND $rows AS row
MATCH (o:KustomizeOverlay {uid: row.source_uid})
MATCH (b:KustomizeOverlay {uid: row.target_uid})
MERGE (o)-[r:EXTENDS_BASE]->(b)
SET r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id`

// retractKustomizeExtendsBaseEdgesCypher deletes stale EXTENDS_BASE edges from
// this cycle's rebuilt repo overlay set's source uids, mirroring
// retractGitlabNeedsEdgesCypher. Scoped to every overlay in the rebuild set
// (not only the uids touched this cycle, see kustomizeRetractSourceUIDs), so
// an overlay that lost its last base -- or whose target base was deleted this
// cycle -- still has its stale edge dropped even though it produces no MERGE
// row itself. Drain-marked for the same mixed structural_edges autocommit
// path as the GitLab retracts: a grouped relationship DELETE can no-op inside
// the structural_edges ExecuteWrite transaction, so this runs as a standalone
// autocommit statement before the sibling MERGE.
const retractKustomizeExtendsBaseEdgesCypher = `UNWIND $source_uids AS uid
MATCH (o:KustomizeOverlay {uid: uid})-[r:EXTENDS_BASE]->(:KustomizeOverlay)
WHERE r.evidence_source = 'projector/canonical' AND r.generation_id <> $generation_id
DELETE r`

// canonicalKustomizeOverlayBaseRefsSetCypher persists a touched overlay's
// base_refs as its own explicit node property. Deliberately never named
// "bases": that key already exists today, with a different meaning, on
// Class/Interface/Trait/etc. nodes as their class-inheritance parent-class
// list (go/internal/parser/python/language.go and siblings), reaching the
// graph through the generic content-entity metadata passthrough
// (canonicalEntityMetadataProperties). Writing base_refs explicitly, instead
// of relying on that passthrough, keeps this contract-owned field
// unambiguous and independent of the generic pipeline's reserved-key policy.
//
// MERGE, not MATCH, anchors the row despite the KustomizeOverlay node always
// already existing by the time this statement runs (created earlier in the
// same materialization's entities phase): reproduced directly against the
// pinned NornicDB backend (isolated Compose project, torn down after
// capture) that `UNWIND $rows AS row MATCH (n:Label {uid: row.uid}) SET
// n.prop = row.val` (bare MATCH, no MERGE, no other clause) silently applies
// no SET at all, regardless of same- or cross-transaction timing, while the
// byte-identical shape with MERGE in place of MATCH persists correctly every
// time. The uid MERGE key makes this a true match-not-create in practice
// (the node already exists), so behavior is unaffected beyond routing around
// the defect. Matches this codebase's existing MERGE-over-bare-MATCH
// precedent for UNWIND-batched property writes (e.g.
// canonicalNodeGitlabNeedsEdgeCypher never bare-MATCHes its own anchors
// either).
const canonicalKustomizeOverlayBaseRefsSetCypher = `UNWIND $rows AS row
MERGE (ko:KustomizeOverlay {uid: row.uid})
SET ko.base_refs = row.base_refs`

// collectKustomizeOverlayEntities extracts KustomizeOverlay rows from the
// materialization's touched entities, mirroring collectGitlabJobEntities:
// label-filtered first (a Class row's unrelated "bases" metadata is never
// read on this path), then base_refs coerced with semanticMetadataStringSlice
// -- NOT metadataString, which only handles a plain string and would
// silently drop the []any shape "bases" arrives as after the fact
// envelope's JSON round trip.
func collectKustomizeOverlayEntities(entities []projector.EntityRow) []KustomizeOverlayRow {
	var rows []KustomizeOverlayRow
	for _, entity := range entities {
		if entity.Label != "KustomizeOverlay" {
			continue
		}
		rows = append(rows, KustomizeOverlayRow{
			UID:      entity.EntityID,
			Path:     entity.FilePath,
			BaseRefs: semanticMetadataStringSlice(entity.Metadata, "bases"),
		})
	}
	return rows
}

// kustomizeOverlayDeletedFilePaths filters a materialization's
// DeltaDeletedFilePaths down to the ones that name a kustomization.yaml/.yml
// file, so a deleted base's stale EXTENDS_BASE edges are considered for
// retraction even though the delta carries no KustomizeOverlay entity for it.
func kustomizeOverlayDeletedFilePaths(deletedFilePaths []string) []string {
	var out []string
	for _, filePath := range deletedFilePaths {
		if isKustomizationFilePath(filePath) {
			out = append(out, filePath)
		}
	}
	return out
}

func isKustomizationFilePath(filePath string) bool {
	base := strings.ToLower(path.Base(filePath))
	return base == "kustomization.yaml" || base == "kustomization.yml"
}

// kustomizeOverlayDirectory returns the repo-relative directory containing
// overlayPath, normalized so the repo root itself is "" (path.Dir returns
// "." for a root-level file, which would not directory-equality-match a
// resolved base of "").
func kustomizeOverlayDirectory(overlayPath string) string {
	dir := path.Dir(overlayPath)
	if dir == "." {
		return ""
	}
	return dir
}

// kustomizeBaseDirectory resolves baseRef relative to overlayPath's own
// directory: path.Clean(dir(overlay)/baseRef). Matching happens by DIRECTORY
// EQUALITY against sibling KustomizeOverlay paths in the caller -- this
// function never guesses a kustomization.yaml/.yml filename to append. A
// reference that walks above the repository root (more ".." segments than
// the overlay's own depth provides) is rejected: ok is false and the caller
// must drop the candidate rather than resolve it.
func kustomizeBaseDirectory(overlayPath, baseRef string) (dir string, ok bool) {
	overlayDir := path.Dir(overlayPath)
	cleaned := path.Clean(path.Join(overlayDir, baseRef))
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", false
	}
	if cleaned == "." {
		cleaned = ""
	}
	return cleaned, true
}

// kustomizeOverlayDirToUID maps each directory to the uid of the
// KustomizeOverlay node occupying it, deterministically. Iterating byUID
// (a Go map) directly to populate this would be nondeterministic run-to-run
// on a directory collision -- two KustomizeOverlay nodes whose paths resolve
// to the same directory. Kustomize itself forbids this (one kustomization
// file per directory), but Eshu ingests untrusted or malformed repos and the
// only schema guard is ko.path uniqueness, never directory uniqueness, so a
// collision is a real, if rare, input shape. Sorting uids ascending first
// and keeping only the first-seen (smallest) uid per directory makes the
// winner a stable function of the data, never of map iteration order.
func kustomizeOverlayDirToUID(byUID map[string]KustomizeOverlayRow) map[string]string {
	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)

	dirToUID := make(map[string]string, len(byUID))
	for _, uid := range uids {
		dir := kustomizeOverlayDirectory(byUID[uid].Path)
		if _, collided := dirToUID[dir]; collided {
			continue // keep the lexicographically smallest uid already recorded
		}
		dirToUID[dir] = uid
	}
	return dirToUID
}

// kustomizeRetractSourceUIDs returns the deduplicated union of every overlay
// uid the rebuilt repo set considered -- both the resolver's persisted rows
// and this cycle's touched rows -- so the retract statement scopes to the
// FULL current repo overlay set, not only the uids touched this cycle. This
// is deliberately broader than gitlabJobSourceUIDs' precedent (scoped to one
// materialization's own entities): an overlay whose target base was deleted
// this cycle needs its stale edge dropped even when the overlay's own file
// was not touched.
func kustomizeRetractSourceUIDs(persisted, touched []KustomizeOverlayRow) []string {
	seen := make(map[string]struct{}, len(persisted)+len(touched))
	var uids []string
	add := func(uid string) {
		if uid == "" {
			return
		}
		if _, dup := seen[uid]; dup {
			return
		}
		seen[uid] = struct{}{}
		uids = append(uids, uid)
	}
	for _, row := range persisted {
		add(row.UID)
	}
	for _, row := range touched {
		add(row.UID)
	}
	return uids
}

// kustomizeOverlayBaseRefPropertyRows builds the base_refs SET rows for every
// touched overlay only -- an untouched overlay's base_refs is already
// persisted from the cycle that touched it and cannot be stale, since its own
// file did not change this cycle (the #5445 backfill semantic: an overlay
// projected before this feature has no base_refs until its own file is next
// touched, or the repo's next full reconciliation projection runs).
func kustomizeOverlayBaseRefPropertyRows(touched []KustomizeOverlayRow) []map[string]any {
	if len(touched) == 0 {
		return nil
	}
	rows := make([]map[string]any, 0, len(touched))
	for _, row := range touched {
		baseRefs := row.BaseRefs
		if baseRefs == nil {
			baseRefs = []string{}
		}
		rows = append(rows, map[string]any{
			"uid":       row.UID,
			"base_refs": baseRefs,
		})
	}
	return rows
}

// kustomizeExtendsBaseEdgeStatements builds the #5445 EXTENDS_BASE edge set
// (overlay -> local base, same repo) plus the base_refs node-property write,
// for every KustomizeOverlay touched or delta-deleted this materialization
// cycle. See KustomizeOverlayResolver's doc comment for why a full-repo read
// is required rather than a same-cycle Go-side join.
//
// Property write and edge rebuild are independent: the base_refs SET runs for
// touched overlays regardless of resolver wiring (it needs no live read), but
// the edge rebuild fails closed -- returns no retract/merge statements -- when
// no resolver is wired or the resolver read errors, since writing a partial
// or wrong edge set is worse than writing none this cycle. A repo with no
// Kustomize overlays touched or deleted this cycle returns nil immediately,
// so this is a no-op for every non-Kustomize repository.
func (w *CanonicalNodeWriter) kustomizeExtendsBaseEdgeStatements(
	ctx context.Context,
	mat projector.CanonicalMaterialization,
) []Statement {
	touched := collectKustomizeOverlayEntities(mat.Entities)
	deletedPaths := kustomizeOverlayDeletedFilePaths(mat.DeltaDeletedFilePaths)
	if len(touched) == 0 && len(deletedPaths) == 0 {
		return nil
	}

	var stmts []Statement
	if rows := kustomizeOverlayBaseRefPropertyRows(touched); len(rows) > 0 {
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     canonicalKustomizeOverlayBaseRefsSetCypher,
			Parameters: map[string]any{"rows": rows},
		})
	}

	if w.kustomizeOverlayResolver == nil {
		return stmts
	}
	persisted, err := w.kustomizeOverlayResolver.ListKustomizeOverlays(ctx, mat.RepoID)
	if err != nil {
		slog.WarnContext(
			ctx, "kustomize overlay resolver failed; skipping EXTENDS_BASE rebuild this cycle (fail closed)",
			"repo_id", mat.RepoID,
			"generation_id", mat.GenerationID,
			"error", err.Error(),
		)
		return stmts
	}

	// Merge persisted (prior-cycle) rows with this cycle's fresh touched rows,
	// keyed by uid -- touched data wins where both exist, since it is fresher
	// than whatever the resolver's read (against pre-this-cycle graph state)
	// returned. Rows whose path was delta-deleted this cycle are dropped
	// entirely: they no longer exist and must not resolve as an edge target.
	byUID := make(map[string]KustomizeOverlayRow, len(persisted)+len(touched))
	for _, row := range persisted {
		byUID[row.UID] = row
	}
	deletedSet := make(map[string]struct{}, len(deletedPaths))
	for _, deletedPath := range deletedPaths {
		deletedSet[deletedPath] = struct{}{}
	}
	for uid, row := range byUID {
		if _, deleted := deletedSet[row.Path]; deleted {
			delete(byUID, uid)
		}
	}
	for _, row := range touched {
		byUID[row.UID] = row
	}

	dirToUID := kustomizeOverlayDirToUID(byUID)

	var mergeRows []map[string]any
	for _, row := range byUID {
		for _, baseRef := range row.BaseRefs {
			resolvedDir, ok := kustomizeBaseDirectory(row.Path, baseRef)
			if !ok {
				continue
			}
			targetUID, ok := dirToUID[resolvedDir]
			if !ok {
				continue // dangling base: no sibling KustomizeOverlay at that directory -- drop silently, never MERGE a placeholder base node.
			}
			mergeRows = append(mergeRows, map[string]any{
				"source_uid":    row.UID,
				"target_uid":    targetUID,
				"generation_id": mat.GenerationID,
			})
		}
	}

	if retractUIDs := kustomizeRetractSourceUIDs(persisted, touched); len(retractUIDs) > 0 {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    retractKustomizeExtendsBaseEdgesCypher,
			Parameters: map[string]any{
				"source_uids":   retractUIDs,
				"generation_id": mat.GenerationID,
			},
			Drain: true,
		})
	}
	if len(mergeRows) > 0 {
		stmts = append(stmts, Statement{
			Operation:  OperationCanonicalUpsert,
			Cypher:     canonicalKustomizeExtendsBaseEdgeCypher,
			Parameters: map[string]any{"rows": mergeRows},
		})
	}
	return stmts
}

// KustomizeOverlayResolverConfigured reports whether the #5445
// KustomizeOverlayResolver is wired on this writer. Mirrors
// TerraformStateResolversConfigured (canonical_node_writer_tfstate_resolvers.go):
// cmd/*-level wiring tests type-assert their constructed
// projector.CanonicalWriter to *CanonicalNodeWriter and call this accessor to
// prove the deployed construction path actually attaches the resolver, not
// just that the isolated adapter type behaves correctly in unit tests.
func (w *CanonicalNodeWriter) KustomizeOverlayResolverConfigured() bool {
	if w == nil {
		return false
	}
	return w.kustomizeOverlayResolver != nil
}
