package cypher

// Cypher templates for canonical node projection phases.
// These are used by CanonicalNodeWriter in strict phase order.

// --- Phase A: Retraction Cypher ---

const canonicalNodeRetractFilesCypher = `MATCH (f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
DETACH DELETE f`

const canonicalNodeRetractRemovedFilesCypher = `MATCH (f:File)
WHERE f.repo_id = $repo_id AND f.evidence_source = 'projector/canonical' AND f.generation_id <> $generation_id
  AND (f.path IS NULL OR NOT (f.path IN $file_paths))
DETACH DELETE f`

const canonicalNodeRetractEntityTemplate = `MATCH (n:%s)
WHERE n.repo_id = $repo_id AND n.evidence_source = 'projector/canonical' AND n.generation_id <> $generation_id
DETACH DELETE n`

const canonicalNodeRetractDirectoriesCypher = `MATCH (d:Directory)
WHERE d.repo_id = $repo_id AND d.generation_id <> $generation_id
  AND (d.path IS NULL OR NOT (d.path IN $directory_paths))
DETACH DELETE d`

const canonicalNodeRefreshCurrentFileImportEdgesCypher = `MATCH (f:File)-[r:IMPORTS]->(:Module)
WHERE f.path IN $file_paths
DELETE r`

const canonicalNodeRefreshCurrentDirectoryFileEdgesCypher = `MATCH (:Directory)-[r:CONTAINS]->(f:File)
WHERE f.path IN $file_paths
DELETE r`

const canonicalNodeRefreshCurrentEntityContainmentEdgesTemplate = `UNWIND $rows AS row
MATCH (n:%s {uid: row.parent_entity_id})-[r:CONTAINS]->(m)
WHERE n.evidence_source = 'projector/canonical'
  AND m.evidence_source = 'projector/canonical'
  AND (m.uid IS NULL OR NOT (m.uid IN row.child_entity_ids))
DELETE r`

const canonicalNodeRetractParametersCypher = `MATCH (p:Parameter)
WHERE p.path IN $file_paths AND p.evidence_source = 'projector/canonical'
  AND p.generation_id <> $generation_id
DETACH DELETE p`

// --- Phase B: Repository Cypher ---

const canonicalNodeRepositoryIDCleanupCypher = `MATCH (r:Repository {id: $repo_id})
DETACH DELETE r`

const canonicalNodeRepositoryPathCleanupCypher = `MATCH (r:Repository {path: $path})
WHERE r.id <> $repo_id
DETACH DELETE r`

const canonicalNodeRepositoryUpsertCypher = `MERGE (r:Repository {id: $repo_id})
SET r.name = $name, r.path = $path, r.local_path = $local_path,
    r.remote_url = $remote_url, r.repo_slug = $repo_slug,
    r.has_remote = $has_remote, r.scope_id = $scope_id,
    r.generation_id = $generation_id,
    r.evidence_source = 'projector/canonical'`

// --- Phase C: Directory Cypher ---

const canonicalNodeDirectoryDepth0Cypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
MERGE (d:Directory {path: row.path})
SET d.name = row.name, d.repo_id = row.repo_id,
    d.scope_id = row.scope_id, d.generation_id = row.generation_id
MERGE (r)-[rel:CONTAINS]->(d)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const canonicalNodeDirectoryDepthNCypher = `UNWIND $rows AS row
MATCH (p:Directory {path: row.parent_path})
MERGE (d:Directory {path: row.path})
SET d.name = row.name, d.repo_id = row.repo_id,
    d.scope_id = row.scope_id, d.generation_id = row.generation_id
MERGE (p)-[rel:CONTAINS]->(d)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

// --- Phase D: File Cypher ---

const canonicalNodeFileUpdateExistingCypher = `UNWIND $rows AS row
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

const canonicalNodeFileCreateMissingCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
MATCH (d:Directory {path: row.dir_path})
WHERE NOT EXISTS { MATCH (:File {path: row.path}) }
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

const canonicalNodeRootFileUpdateExistingCypher = `UNWIND $rows AS row
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

const canonicalNodeRootFileCreateMissingCypher = `UNWIND $rows AS row
MATCH (r:Repository {id: row.repo_id})
WHERE NOT EXISTS { MATCH (:File {path: row.path}) }
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

// canonicalNodeEntityUpsertTemplate is formatted with the graph label at write
// time. It intentionally writes only the entity node so rows can batch across
// files and stay aligned with NornicDB's simple UNWIND/MERGE hot path.
const canonicalNodeEntityUpsertTemplate = `UNWIND $rows AS row
MERGE (n:%s {uid: row.entity_id})
SET n += row.props`

const canonicalNodeEntitySingletonUpsertTemplate = `MERGE (n:%s {uid: $entity_id})
SET n += $props`

const canonicalNodeEntityFileScopedUpsertWithContainmentTemplate = `UNWIND $rows AS row
MATCH (f:File {path: $file_path})
MERGE (n:%s {uid: row.entity_id})
SET n += row.props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const canonicalNodeEntityUpsertWithContainmentTemplate = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MERGE (n:%s {uid: row.entity_id})
SET n += row.props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const canonicalNodeEntitySingletonUpsertWithContainmentTemplate = `MATCH (f:File {path: $file_path})
MERGE (n:%s {uid: $entity_id})
SET n += $props
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = $generation_id`

const canonicalNodeEntityContainmentEdgeTemplate = `UNWIND $rows AS row
MATCH (f:File {path: $file_path})
MATCH (n:%s {uid: row.entity_id})
MERGE (f)-[rel:CONTAINS]->(n)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

// --- Phase F: Module Cypher ---

const canonicalNodeModuleUpsertCypher = `UNWIND $rows AS row
MERGE (m:Module {name: row.name})
ON CREATE SET m.lang = row.language
ON MATCH SET m.lang = coalesce(m.lang, row.language)`

// --- Phase G: Structural edge Cypher ---

const canonicalNodeImportEdgeCypher = `UNWIND $rows AS row
MATCH (f:File {path: row.file_path})
MATCH (m:Module {name: row.module_name})
MERGE (f)-[r:IMPORTS]->(m)
SET r.imported_name = row.imported_name, r.alias = row.alias, r.line_number = row.line_number,
    r.evidence_source = 'projector/canonical', r.generation_id = row.generation_id`

const canonicalNodeHasParameterEdgeCypher = `UNWIND $rows AS row
MATCH (fn:Function {name: row.func_name, path: row.file_path, line_number: row.func_line})
MERGE (p:Parameter {name: row.param_name, path: row.file_path, function_line_number: row.func_line})
MERGE (fn)-[rel:HAS_PARAMETER]->(p)
SET p.evidence_source = 'projector/canonical',
    p.generation_id = row.generation_id,
    rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const canonicalNodeClassContainsFuncEdgeCypher = `UNWIND $rows AS row
MATCH (c:Class {name: row.class_name, path: row.file_path})
MATCH (fn:Function {name: row.func_name, path: row.file_path, line_number: row.func_line})
MERGE (c)-[rel:CONTAINS]->(fn)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`

const canonicalNodeNestedFuncEdgeCypher = `UNWIND $rows AS row
MATCH (outer:Function {name: row.outer_name, path: row.file_path})
MATCH (inner:Function {name: row.inner_name, path: row.file_path, line_number: row.inner_line})
MERGE (outer)-[rel:CONTAINS]->(inner)
SET rel.evidence_source = 'projector/canonical',
    rel.generation_id = row.generation_id`
