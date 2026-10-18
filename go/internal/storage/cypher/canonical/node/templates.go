// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

// Cypher templates for canonical node projection phases.
// These are used by CanonicalNodeWriter in strict phase order.

// --- Phase A: Retraction Cypher ---

const RetractFilesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`

const RetractRemovedFilesCypher = `MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
  AND (f.path IS NULL OR NOT (f.path IN $file_paths))
DETACH DELETE f`

// The delta file/directory DELETE statements below seed their worklist with
// a positive `IN $file_paths` / `IN $directory_paths` predicate, never an
// `UNWIND ... AS ...` seed plus compound WHERE. Measured on the pinned
// NornicDB v1.3.3 backend at 600k-node scale, the UNWIND-seeded compound
// shape costs 87-216s per execution while the IN shape costs 0.1-9s with an
// identical deleted row set (issue #6715). The phase-group chunker splits
// both shapes at the same batch size, and the IN shape is the one proven
// grouped-transaction-safe (see nornicdb-pitfalls.md), so the rewrite keeps
// batching, replay safety, and the anchor-direction rule (every traversal
// still expands from its bound variable).
const RetractDeltaDeletedFilesCypher = `MATCH (f:File)
WHERE f.path IN $file_paths
  AND f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical'
DETACH DELETE f`

const RetractDeltaDeletedDirectoryEdgesCypher = `MATCH (d:Directory)
WHERE d.path IN $directory_paths
  AND d.repo_id = $repo_id
  AND d.evidence_source = 'projector/canonical'
MATCH (d)-[r:CONTAINS]-()
DELETE r`

const RetractDeltaDeletedDirectoriesCypher = `MATCH (d:Directory)
WHERE d.path IN $directory_paths
  AND d.repo_id = $repo_id
  AND d.evidence_source = 'projector/canonical'
DETACH DELETE d`

const RetractDeltaEmptyDirectoriesCypher = `MATCH (d:Directory)
WHERE d.path IN $directory_paths
  AND d.repo_id = $repo_id
  AND NOT EXISTS { MATCH (d)-[:CONTAINS]->(:File) }
  AND NOT EXISTS { MATCH (d)-[:CONTAINS]->(:Directory) }
DETACH DELETE d`

const RetractEntityTemplate = `MATCH (n:%s)
WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id
DETACH DELETE n`

const RetractDeltaEntityTemplate = `MATCH (n:%s)
WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical'
  AND n.path IN $file_paths AND n.generation_id <> $generation_id
DETACH DELETE n`

const RetractDirectoriesCypher = `MATCH (d:Directory)
WHERE d.repo_id = $repo_id AND d.generation_id <> $generation_id
  AND (d.path IS NULL OR NOT (d.path IN $directory_paths))
DETACH DELETE d`

const RefreshCurrentFileImportEdgesCypher = `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
MATCH (f)-[r:IMPORTS]->(:Module)
DELETE r`

// RefreshCurrentDirectoryFileEdgesCypher and
// RefreshCurrentDirectoryParentEdgesCypher both walk a CONTAINS
// edge INTO a node the statement has already anchored by indexed path. Both are
// written from the anchored node outward, with a `<-` arrow, rather than from
// the unbound `(:Directory)` side.
//
// The direction is load-bearing on NornicDB, not style. Written the other way
// round — `MATCH (:Directory)-[r:CONTAINS]->(f)` — the executor cold-plans a
// full Directory label scan and re-joins the CONTAINS fanout to `f`, so the
// cost tracks the whole graph's Directory population instead of the handful of
// paths in `$file_paths`. That is what dead-lettered eleven projector work
// items in the #4207 full-corpus run: repositories as small as 57 facts blew
// the same 120s budget as one with 59,717, and only on the second generation,
// once the corpus was populated. Chunking cannot help, because the cost is per
// input row and independent of how many rows a chunk carries. Reading from the
// anchored side instead matches the identical edges out of that node's own
// adjacency. Same rewrite as the workload catalog queries in #3466 and #1731.
const RefreshCurrentDirectoryFileEdgesCypher = `UNWIND $file_paths AS file_path
MATCH (f:File {path: file_path})
MATCH (f)<-[r:CONTAINS]-(:Directory)
DELETE r`

const RefreshCurrentDirectoryParentEdgesCypher = `UNWIND $rows AS row
MATCH (d:Directory {path: row.path})<-[r:CONTAINS]-(p:Directory)
WHERE d.repo_id = $repo_id
  AND d.evidence_source = 'projector/canonical'
  AND p.path <> row.parent_path
DELETE r`

const RefreshCurrentEntityContainmentEdgesTemplate = `UNWIND $rows AS row
MATCH (n:%s {uid: row.parent_entity_id})-[r:CONTAINS]->(m)
WHERE n.evidence_source = 'projector/canonical'
  AND m.evidence_source = 'projector/canonical'
  AND (m.uid IS NULL OR NOT (m.uid IN row.child_entity_ids))
DELETE r`

const RetractParametersCypher = `MATCH (p:Parameter)
WHERE p.path IN $file_paths AND p.evidence_source = 'projector/canonical'
  AND p.generation_id <> $generation_id
DETACH DELETE p`

// --- Phase B: Repository Cypher ---

// There is deliberately no by-id Repository delete here (#7285): the node is
// MERGEd in place so edges owned by other writers survive re-projection.

// RepositoryPathCleanupCypher retires a DIFFERENT-id
// Repository still holding the projected path (#7324). The incident-edge
// collection runs in the SAME statement, before the delete, so the
// attribution describes exactly the relationships the delete drops: a
// separate pre-read would race a concurrent writer between the read and the
// delete, and the Executor interfaces (writer.go) cannot return records.
//
// Shape notes: the OPTIONAL MATCH keeps the one-row-per-edge fanout off the
// delete (an edgeless node still yields its single row, so the delete still
// runs); the CASE keeps NULL rels out of the collection (collect skips
// NULL), so an edgeless retirement reports an empty dropped list rather
// than one junk entry; startNode decides direction from the stored edge,
// not from a pattern arrow. retired_id is bound in WITH, before the delete:
// the RETURN reads a bound scalar, never a property off the deleted node.
// RETURN yields one record per retired node when the MATCH matched, zero
// records when it matched nothing (steady state).
//
// Neo4j-only: NornicDB routes this multi-clause composition into an
// interpreter that drops the WHERE guard and deletes the path-matched live
// node in steady state, and corrupts the RETURN projection to the literal
// "dropped" (live probe #8173: steady state deleted 1 node + 1
// relationship, record value "dropped"). Writers targeting NornicDB MUST
// emit RepositoryPathCleanupBareCypher instead (see
// WithBareRepositoryRetirement): the bare shape is correct on both
// backends, and its entries report through the unattributed
// (attribution=false) path. A NornicDB retirement therefore reports no
// retired id; its fingerprint invalidation comes from the #8316 gap
// reconciliation, not from this RETURN.
const RepositoryPathCleanupCypher = `MATCH (r:Repository {path: $path})
WHERE r.id <> $repo_id
OPTIONAL MATCH (r)-[rel]-(other)
WITH r, r.id AS retired_id, collect(CASE WHEN rel IS NULL THEN NULL ELSE {t: type(rel), dir: CASE WHEN startNode(rel) = r THEN 'out' ELSE 'in' END, src: rel.evidence_source} END) AS dropped
DETACH DELETE r
RETURN retired_id, dropped`

// RepositoryPathCleanupBareCypher is the pre-split retirement
// shape: the same path anchor and id guard, no incident-edge collection, no
// RETURN record. It is correct on both backends (live probe: steady state
// deletes 0 nodes on NornicDB and Neo4j) and is the shape every
// NornicDB-targeted writer emits. Its entries carry no Dropped collection,
// so the report logs attribution=false and moves only
// eshu_dp_canonical_repository_retirements_total.
const RepositoryPathCleanupBareCypher = `MATCH (r:Repository {path: $path})
WHERE r.id <> $repo_id
DETACH DELETE r`

const RepositoryUpsertCypher = `MERGE (r:Repository {id: $repo_id})
SET r.name = $name, r.path = $path, r.local_path = $local_path,
    r.remote_url = $remote_url, r.repo_slug = $repo_slug,
    r.has_remote = $has_remote, r.scope_id = $scope_id,
    r.generation_id = $generation_id,
    r.evidence_source = 'projector/canonical'`

// --- Phase C: Directory Cypher ---
//
// Directory writes are split into a node phase and an edge phase. The node
// phase MERGEs every Directory by path with NO MATCH, so it carries no
// cross-row visibility dependency. The edge phase wires each Directory to its
// parent (Repository for depth-0, the parent Directory for depth-N) and runs as
// a LATER phase, after the node phase commits.
//
// The split is required by the NornicDB phase-group executor (the production
// projector + B-7 gate path): it runs each write phase as one transaction that
// does NOT give a later statement's `MATCH` read-your-writes against an earlier
// statement's MERGE in the same phase. The previous combined write emitted the
// depth-N directories as a separate statement that `MATCH (p:Directory {path:
// parent_path})`'d the depth-(N-1) directory MERGE'd by an earlier statement in
// the same `directories` phase; that MATCH found nothing, so the depth-N
// directory was never created and every file (and its entities) nested beneath
// it was silently dropped. Committing the nodes in their own phase before the
// edge phase resolves it. (The atomic GroupExecutor path DOES give single-label
// cross-statement read-your-writes — see the RequireAtomicGroup conformance
// cases — so the edge phase stays inline there; only multi-label package_registry
// edges are deferred. Neo4j is unaffected.)

const DirectoryNodeCypher = `UNWIND $rows AS row
MERGE (d:Directory {path: row.path})
SET d.name = row.name, d.repo_id = row.repo_id,
    d.scope_id = row.scope_id, d.generation_id = row.generation_id,
    d.evidence_source = 'projector/canonical'`

const DirectoryDepth0EdgeCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
MATCH (d:Directory {path: row.path})
MERGE (r)-[rel:CONTAINS]->(d)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const DirectoryDepthNEdgeCypher = `UNWIND $rows AS row
MATCH (p:Directory {path: row.parent_path})
MATCH (d:Directory {path: row.path})
MERGE (p)-[rel:CONTAINS]->(d)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

// --- Phase D: File Cypher ---

const FileUpdateExistingCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.path})
SET f.name = row.name, f.relative_path = row.relative_path,
    f.uid = row.uid,
    f.language = row.language, f.lang = row.language,
    f.repo_id = row.repo_id,
    f.scope_id = row.scope_id, f.generation_id = row.generation_id,
    f.evidence_source = 'projector/canonical'
WITH f, row
MATCH (r:Repository {id: row.repo_id})
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = row.generation_id
WITH f, row
MATCH (d:Directory {path: row.dir_path})
MERGE (d)-[dirRel:CONTAINS]->(f)
SET dirRel.evidence_source = 'projector/canonical',
    dirRel.generation_id = row.generation_id`

// The create_missing templates skip files that already exist with a
// correlated, index-backed OPTIONAL MATCH on File.path. Do not use
// NOT EXISTS { MATCH (:File {path: row.path}) }: NornicDB v1.3.3 evaluates an
// uncorrelated existence subquery by loading every File node per row and never
// matches row.path, so it scanned the whole label and re-stamped existing
// files (#6798).
const FileCreateMissingCypher = `UNWIND $rows AS row
OPTIONAL MATCH (existing:File {path: row.path})
WITH row, existing
WHERE existing IS NULL
MATCH (r:Repository {id: row.repo_id})
MATCH (d:Directory {path: row.dir_path})
MERGE (f:File {path: row.path})
SET f.name = row.name, f.relative_path = row.relative_path,
    f.uid = row.uid,
    f.language = row.language, f.lang = row.language,
    f.repo_id = row.repo_id,
    f.scope_id = row.scope_id, f.generation_id = row.generation_id,
    f.evidence_source = 'projector/canonical'
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = row.generation_id
MERGE (d)-[dirRel:CONTAINS]->(f)
SET dirRel.evidence_source = 'projector/canonical',
    dirRel.generation_id = row.generation_id`

const FileFirstGenerationMergeCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
MATCH (d:Directory {path: row.dir_path})
MERGE (f:File {path: row.path})
SET f.name = row.name, f.relative_path = row.relative_path,
    f.uid = row.uid,
    f.language = row.language, f.lang = row.language,
    f.repo_id = row.repo_id,
    f.scope_id = row.scope_id, f.generation_id = row.generation_id,
    f.evidence_source = 'projector/canonical'
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = row.generation_id
MERGE (d)-[dirRel:CONTAINS]->(f)
SET dirRel.evidence_source = 'projector/canonical',
    dirRel.generation_id = row.generation_id`

const RootFileUpdateExistingCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.path})
SET f.name = row.name, f.relative_path = row.relative_path,
    f.uid = row.uid,
    f.language = row.language, f.lang = row.language,
    f.repo_id = row.repo_id,
    f.scope_id = row.scope_id, f.generation_id = row.generation_id,
    f.evidence_source = 'projector/canonical'
WITH f, row
MATCH (r:Repository {id: row.repo_id})
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = row.generation_id`

const RootFileCreateMissingCypher = `UNWIND $rows AS row
OPTIONAL MATCH (existing:File {path: row.path})
WITH row, existing
WHERE existing IS NULL
MATCH (r:Repository {id: row.repo_id})
MERGE (f:File {path: row.path})
SET f.name = row.name, f.relative_path = row.relative_path,
    f.uid = row.uid,
    f.language = row.language, f.lang = row.language,
    f.repo_id = row.repo_id,
    f.scope_id = row.scope_id, f.generation_id = row.generation_id,
    f.evidence_source = 'projector/canonical'
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = row.generation_id`

const RootFileFirstGenerationMergeCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
MERGE (f:File {path: row.path})
SET f.name = row.name, f.relative_path = row.relative_path,
    f.uid = row.uid,
    f.language = row.language, f.lang = row.language,
    f.repo_id = row.repo_id,
    f.scope_id = row.scope_id, f.generation_id = row.generation_id,
    f.evidence_source = 'projector/canonical'
MERGE (r)-[repoRel:REPO_CONTAINS]->(f)
SET repoRel.evidence_source = 'projector/canonical',
    repoRel.generation_id = row.generation_id`

// --- Phase E: Entity Cypher (template — label inserted via fmt.Sprintf) ---

// EntityUpsertTemplate is formatted with the graph label at write
// time. It intentionally writes only the entity node so rows can batch across
// files and stay aligned with NornicDB's simple UNWIND/MERGE hot path.
// anchor-census: dynamic-label writer; label set bounded by TestEntityUpsertTemplateLabelsAreAnchorLabels
const EntityUpsertTemplate = `UNWIND $rows AS row
MERGE (n:%s {uid: row.entity_id})
SET n += row.props`

// anchor-census: dynamic-label writer; label set bounded by TestEntityUpsertTemplateLabelsAreAnchorLabels
const EntitySingletonUpsertTemplate = `MERGE (n:%s {uid: $entity_id})
SET n += $props`

// anchor-census: dynamic-label writer; label set bounded by TestEntityUpsertTemplateLabelsAreAnchorLabels
const EntityFileScopedUpsertWithContainmentTemplate = `UNWIND $rows AS row
MATCH (f:File {path: $file_path})
MERGE (n:%s {uid: row.entity_id})
SET n += row.props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

// anchor-census: dynamic-label writer; label set bounded by TestEntityUpsertTemplateLabelsAreAnchorLabels
const EntityUpsertWithContainmentTemplate = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:%s {uid: row.entity_id})
SET n += row.props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

// anchor-census: dynamic-label writer; label set bounded by TestEntityUpsertTemplateLabelsAreAnchorLabels
const EntitySingletonUpsertWithContainmentTemplate = `MATCH (f:File {path: $file_path})
MERGE (n:%s {uid: $entity_id})
SET n += $props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = $generation_id`

const EntityContainmentEdgeTemplate = `UNWIND $rows AS row
MATCH (f:File {path: $file_path})
MATCH (n:%s {uid: row.entity_id})
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

// --- Phase F: Module Cypher ---

// ModuleUpsertCypher MERGEs the import-graph Module node on
// (name, lang), which is its identity: a Go `time` and a Python `time` are
// unrelated modules that share a name, while two repositories importing the
// same Go `time` share one node. Repository and scope are deliberately NOT in
// the key -- that shared node IS the cross-repository dependency link, and the
// relationships catalog records the same intent by marking IMPORTS
// targetAttributable=false (Module carries no tenant attribution).
//
// Keying on name alone minted one global node per name and left the language
// to `coalesce(m.lang, row.language)`. That is not merely first-writer-wins:
// measured against the pinned NornicDB build, an UNWIND batch carrying both
// languages evaluated every row's SET against the pre-batch property snapshot,
// so the LAST row won and the stamped language tracked batch order. The graph
// then asserted that a Go file imports a Python module, and a query filtering
// `(m:Module {name: 'time', lang: 'go'})` silently missed every Go importer.
//
// The MERGE stays on the schema hot path with the existing single-property
// `module_name_lookup` index: NornicDB's findMergeNode prefers an exact
// composite index, then a unique constraint, then the smallest single-property
// index candidate set, and only falls back to a label scan when NONE of those
// matched. The name index still serves the anchor, and the candidate set is
// identical to the pre-change lookup, so the row is filtered on lang in Go
// rather than scanned for. Module cannot take a uniqueness constraint on name
// (the semantic entity path MERGEs Module on uid and shares this label), which
// is why the index, not a constraint, is the anchor.
const ModuleUpsertCypher = `UNWIND $rows AS row
MERGE (m:Module {name: row.name, lang: row.language})
SET m.evidence_source = 'projector/canonical'`

// CanonicalNodeModuleUpsertCypher is the import-graph Module upsert that
// CanonicalNodeWriter batches with row keys `name` and `language`. It is
// exported only so internal/backendconformance can run the exact production
// statement against both backends; production callers go through the writer.
const CanonicalNodeModuleUpsertCypher = ModuleUpsertCypher

// --- Phase G: Structural edge Cypher ---

// ImportEdgeCypher wires each file to the module it imports. The
// target is resolved on the full (name, lang) Module identity, using the
// importing file's own language: matching on name alone would resolve every
// same-named module and attach one file to all of them, which is a worse
// accuracy failure than the collapsed node it replaced.
//
// type_only, deferred, and inferred (issue #7345) are the parser's import flags
// folded across the (file, module) pair; see foldImportFlags in
// projector/canonical. They are SET unconditionally and always as explicit
// booleans. The MERGE matches on endpoints, so a re-projected edge is an
// existing edge, and only an unconditional SET of an explicit false overwrites a
// stale true; a CASE-guarded or write-only-true SET would keep it. The SET reads
// only the row, never the existing edge, so it is not exposed to the
// pre-batch-snapshot behavior described on ModuleUpsertCypher.
// An edge written before this change has no flag property at all, which a reader
// must treat as "flags unknown", not as false.
//
// source_package (issues #7347/#7348/#7349) is the importing file's own
// package: the Go package import path, the owning npm package name, or
// the owning Java package, derived by the projector. It is SET
// unconditionally and always as an explicit string for the same
// stale-overwrite reason; an edge written before it existed has no
// such property, which a package-cycle reader must treat as
// "package unknown" and exclude, never as an empty package.
const ImportEdgeCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MATCH (m:Module {name: row.module_name, lang: row.module_language})
MERGE (f)-[r:IMPORTS]->(m)
SET r.imported_name = row.imported_name, r.alias = row.alias, r.line_number = row.line_number,
    r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id,
    r.type_only = row.type_only, r.deferred = row.deferred, r.inferred = row.inferred,
    r.source_package = row.source_package`

// HasParameterEdgeCypher upserts Parameter nodes and the
// HAS_PARAMETER relationship from Function to Parameter. The SET assignments
// for p and rel are split into two clauses (one per target variable) so
// NornicDB's executeUnwindMergeChainBatch fast path engages — its
// parseUnwindSimpleSetAssignments helper enforces single-variable SET bodies
// and bails the whole chain-batch plan on a multi-variable SET. The split is
// semantically equivalent per Cypher spec.
const HasParameterEdgeCypher = `UNWIND $rows AS row
MATCH (fn:Function {name: row.func_name, path: row.file_path, line_number: row.func_line})
MERGE (p:Parameter {name: row.param_name, path: row.file_path, function_line_number: row.func_line})
SET p.evidence_source = 'projector/canonical',
    p.generation_id = row.generation_id
MERGE (fn)-[rel:HAS_PARAMETER]->(p)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const ClassContainsFuncEdgeCypher = `UNWIND $rows AS row
MATCH (c:Class {name: row.class_name, path: row.file_path})
MATCH (fn:Function {name: row.func_name, path: row.file_path, line_number: row.func_line})
MERGE (c)-[rel:CONTAINS]->(fn)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const NestedFuncEdgeCypher = `UNWIND $rows AS row
MATCH (outer:Function {name: row.outer_name, path: row.file_path})
MATCH (inner:Function {name: row.inner_name, path: row.file_path, line_number: row.inner_line})
MERGE (outer)-[rel:CONTAINS]->(inner)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`
