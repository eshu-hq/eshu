// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RelationshipGraphRowCypher returns the single-row relationship Cypher
// fragment matching entities with a bare `MATCH (e)` label scan filtered by
// predicate. Use RelationshipGraphRowCypherAnchored instead when the caller
// already knows the entity's repository, so the scan starts from a bounded
// repository->file->entity path rather than a global scan filtered
// afterward. access binds a scoped caller's grant; see
// RelationshipGraphRowCypherFromAnchor.
func RelationshipGraphRowCypher(predicate string, access querycontract.RepositoryAccessFilter) string {
	return RelationshipGraphRowCypherAnchored("MATCH (e)", predicate, access)
}

// RelationshipGraphRowCypherAnchored is RelationshipGraphRowCypher with the
// entity match clause supplied by the caller instead of the bare "MATCH (e)"
// scan -- e.g. "MATCH (anchorRepo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(anchorFile:File)-[:CONTAINS]->(e)"
// to anchor the scan on a known repository.
//
// Issue #6786 defect 2: the repo-filtered relationship lookup used to render
// the repository filter as a backward multi-hop EXISTS spliced into the
// bare-scan predicate ("e.name = $name AND EXISTS { MATCH (e)<-[:CONTAINS]-
// (f:File)<-[:REPO_CONTAINS]-(repo:Repository) WHERE repo.id = $repo_id }").
// NornicDB v1.3.3 silently ignores that backward multi-hop EXISTS, so a
// same-named entity in a different repository still matched and RunSingle
// returned whichever row came back first, regardless of the requested
// repo_id. Anchoring the MATCH itself on the repository -- the same shape
// entity.BuildResolveEntityGraphQuery already uses -- makes the repository
// scope part of the graph traversal instead of a post-hoc existence check,
// which both backends evaluate correctly.
func RelationshipGraphRowCypherAnchored(matchClause, predicate string, access querycontract.RepositoryAccessFilter) string {
	return RelationshipGraphRowCypherFromAnchor(matchClause+` WHERE `+predicate, access)
}

// RelationshipGraphRowCypherFromAnchor is the single-row relationship read
// with the entity-binding clause supplied whole, so a caller can bind e with a
// clause that takes no trailing WHERE -- the Neo4j entity-id read passes
// Neo4jEntityIDAnchor("e", "$entity_id") here (issue #7057).
//
// For a scoped caller the grant binds three times (#5167), each on a node's
// own repo_id, and the caller binds the arrays with access.GraphParams:
//
//   - the anchor, in a `WITH e WHERE` directly after the anchor clause, so an
//     entity outside the grant yields no row at all. This is a required
//     clause, so it decides row membership; the anchor clause may be a
//     CALL () { ... } subquery, which takes no trailing WHERE of its own.
//   - each neighbour, in the WHERE of the OPTIONAL MATCH that binds it. There
//     the WHERE is meant to constrain the optional pattern and not the driving
//     row: an out-of-grant neighbour is not matched, and the anchor row
//     survives with its granted neighbours (or with none). That is the
//     opposite of the #6553 story defect, where a grant predicate sat on an
//     OPTIONAL MATCH it was expected to drop rows through. Measured on
//     neo4j:2026-community in relationships_grant_live_test.go.
//
// A node with no repo_id fails the predicate, which is the fail-closed
// answer: the graph cannot attribute it to a granted repository.
func RelationshipGraphRowCypherFromAnchor(anchorClause string, access querycontract.RepositoryAccessFilter) string {
	anchorGrant, targetGrant, sourceGrant := "", "", ""
	if access.Scoped() {
		anchorGrant = `
		WITH e WHERE ` + access.GraphConditionOnProperty("e", "repo_id")
		targetGrant = ` WHERE ` + access.GraphConditionOnProperty("target", "repo_id")
		sourceGrant = ` WHERE ` + access.GraphConditionOnProperty("source", "repo_id")
	}
	return `
		` + anchorClause + anchorGrant + `
		OPTIONAL MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
		OPTIONAL MATCH (e)-[outgoingRel]->(target)` + targetGrant + `
		OPTIONAL MATCH (target)<-[:CONTAINS]-(targetFile:File)<-[:REPO_CONTAINS]-(targetRepo:Repository)
		OPTIONAL MATCH (source)-[incomingRel]->(e)` + sourceGrant + `
		OPTIONAL MATCH (source)<-[:CONTAINS]-(sourceFile:File)<-[:REPO_CONTAINS]-(sourceRepo:Repository)
		RETURN coalesce(e.id, e.uid) as id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       repo.id as repo_id, repo.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		       ,collect(DISTINCT {
		           direction: 'outgoing',
		           type: type(outgoingRel),
		           call_kind: outgoingRel.call_kind,
		           reason: outgoingRel.reason,
		           confidence: outgoingRel.confidence,
		           resolution_method: outgoingRel.resolution_method,
		           source_name: e.name,
		           source_id: coalesce(e.id, e.uid),
		           source_repo_id: repo.id,
		           source_repo_name: repo.name,
		           source_file_path: f.relative_path,
		           source_language: coalesce(e.language, f.language),
		           source_type: head(labels(e)),
		           source_start_line: e.start_line,
		           source_end_line: e.end_line,
		           target_name: target.name,
		           target_id: coalesce(target.id, target.uid),
		           target_repo_id: targetRepo.id,
		           target_repo_name: targetRepo.name,
		           target_file_path: targetFile.relative_path,
		           target_language: coalesce(target.language, targetFile.language),
		           target_type: head(labels(target)),
		           target_start_line: target.start_line,
		           target_end_line: target.end_line
		       }) as outgoing,
		       collect(DISTINCT {
		           direction: 'incoming',
		           type: type(incomingRel),
		           call_kind: incomingRel.call_kind,
		           reason: incomingRel.reason,
		           confidence: incomingRel.confidence,
		           resolution_method: incomingRel.resolution_method,
		           source_name: source.name,
		           source_id: coalesce(source.id, source.uid),
		           source_repo_id: sourceRepo.id,
		           source_repo_name: sourceRepo.name,
		           source_file_path: sourceFile.relative_path,
		           source_language: coalesce(source.language, sourceFile.language),
		           source_type: head(labels(source)),
		           source_start_line: source.start_line,
		           source_end_line: source.end_line,
		           target_name: e.name,
		           target_id: coalesce(e.id, e.uid),
		           target_repo_id: repo.id,
		           target_repo_name: repo.name,
		           target_file_path: f.relative_path,
		           target_language: coalesce(e.language, f.language),
		           target_type: head(labels(e)),
		           target_start_line: e.start_line,
		           target_end_line: e.end_line
		       }) as incoming
		LIMIT 2
	`
}

// BuildTransitiveRelationshipRowsCypher returns the bounded transitive CALLS
// traversal Cypher under the BFS contract (issue #6849): each reachable node
// once, at its shortest depth, with the start node excluded. The walk
// aggregates to the minimum path length per node and filters the anchor,
// so the Neo4j-compat route returns the same node set as the NornicDB
// breadth-first walk. Both directions stay directed every hop, matching the
// BFS one-hop read.
//
// access binds a scoped caller's grant on the Neo4j branch only (#5167):
// every node on the path, not just its far end, must carry a granted
// repo_id, so the walk never reaches a granted node THROUGH an ungranted
// one -- the same answer the NornicDB breadth-first walk gives by binding
// each hop. The NornicDB branch is not bound: the handler never sends it
// (NornicDB answers transitive CALLS through
// CodeHandler.nornicDBTransitiveRelationshipRows), and the pinned NornicDB
// build does not evaluate a list-membership test inside all(...), so a
// bound variant there would look filtered while returning every row.
func BuildTransitiveRelationshipRowsCypher(
	entityID string,
	direction string,
	maxDepth int,
	backend querycontract.GraphBackend,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{
		"entity_id": strings.TrimSpace(entityID),
	}
	var cypher strings.Builder
	if backend == querycontract.GraphBackendNornicDB {
		if direction == "incoming" {
			cypher.WriteString("\n\t\tMATCH (e)\n")
			cypher.WriteString("\t\tWHERE ")
			cypher.WriteString(GraphEntityIDPredicate("e", "$entity_id"))
			cypher.WriteString("\n\t\tMATCH path = (source)-[:CALLS*1..")
			fmt.Fprint(&cypher, maxDepth)
			cypher.WriteString("]->(e)\n")
			cypher.WriteString("\t\tWHERE source <> e\n")
			cypher.WriteString("\t\tWITH source, min(length(path)) AS depth\n")
			cypher.WriteString("\t\tRETURN source.name as source_name,\n")
			cypher.WriteString("\t\t       coalesce(source.id, source.uid) as source_id,\n")
			cypher.WriteString("\t\t       depth\n")
			cypher.WriteString("\t\tORDER BY depth, source_id\n\t")
			return cypher.String(), params
		}

		cypher.WriteString("\n\t\tMATCH (e)\n")
		cypher.WriteString("\t\tWHERE ")
		cypher.WriteString(GraphEntityIDPredicate("e", "$entity_id"))
		cypher.WriteString("\n\t\tMATCH path = (e)-[:CALLS*1..")
		fmt.Fprint(&cypher, maxDepth)
		cypher.WriteString("]->(target)\n")
		cypher.WriteString("\t\tWHERE target <> e\n")
		cypher.WriteString("\t\tWITH target, min(length(path)) AS depth\n")
		cypher.WriteString("\t\tRETURN target.name as target_name,\n")
		cypher.WriteString("\t\t       coalesce(target.id, target.uid) as target_id,\n")
		cypher.WriteString("\t\t       depth\n")
		cypher.WriteString("\t\tORDER BY depth, target_id\n\t")
		return cypher.String(), params
	}

	// Neo4j anchors on the uid uniqueness constraint of every label a CALLS
	// edge can touch (issue #7057). The unlabeled id-OR-uid predicate the
	// NornicDB branch keeps planned as an AllNodesScan here. Every CALLS writer
	// MATCHes both endpoints by uid on these labels, and the canonical entity
	// writer sets id to the same EntityID as uid, so a node outside the set or
	// matched only by id cannot start a CALLS walk and never produced a row.
	pathGrant := ""
	if access.Scoped() {
		params = access.GraphParams(params)
		pathGrant = " AND all(node IN nodes(path) WHERE " + access.GraphConditionOnProperty("node", "repo_id") + ")"
	}
	cypher.WriteString("\n\t\tMATCH (e:" + CallGraphEndpointLabels + " {uid: $entity_id})\n")
	if direction == "incoming" {
		cypher.WriteString("\t\tMATCH path = (source)-[:CALLS*1..")
		fmt.Fprint(&cypher, maxDepth)
		cypher.WriteString("]->(e)\n")
		cypher.WriteString("\t\tWHERE source <> e" + pathGrant + "\n")
		cypher.WriteString("\t\tWITH source, min(length(path)) AS depth\n")
		cypher.WriteString("\t\tRETURN source.name as source_name,\n")
		cypher.WriteString("\t\t       coalesce(source.id, source.uid) as source_id,\n")
		cypher.WriteString("\t\t       depth\n")
		cypher.WriteString("\t\tORDER BY depth, source_id\n\t")
		return cypher.String(), params
	}

	cypher.WriteString("\t\tMATCH path = (e)-[:CALLS*1..")
	fmt.Fprint(&cypher, maxDepth)
	cypher.WriteString("]->(target)\n")
	cypher.WriteString("\t\tWHERE target <> e" + pathGrant + "\n")
	cypher.WriteString("\t\tWITH target, min(length(path)) AS depth\n")
	cypher.WriteString("\t\tRETURN target.name as target_name,\n")
	cypher.WriteString("\t\t       coalesce(target.id, target.uid) as target_id,\n")
	cypher.WriteString("\t\t       depth\n")
	cypher.WriteString("\t\tORDER BY depth, target_id\n\t")
	return cypher.String(), params
}

// CallGraphEndpointLabels is the Neo4j label disjunction of every node a CALLS
// edge can start or end on. It matches the endpoint labels the canonical
// code-call edge writers MATCH by uid (storage/cypher/edge/writer
// codeCallEndpointLabels and CodeCallRetractSourceLabels), and each label
// carries a uid uniqueness constraint. A MATCH (n:<labels> {uid: $id}) on it
// plans as one NodeUniqueIndexSeek per label.
const CallGraphEndpointLabels = "Function|Class|Struct|Interface|TypeAlias|File"

// GraphEntityIDPredicate returns the entity-identity MATCH predicate for one alias.
func GraphEntityIDPredicate(alias string, param string) string {
	return fmt.Sprintf("(%s.id = %s OR %s.uid = %s)", alias, param, alias, param)
}

// BuildTransitiveRelationshipGraphResponse shapes transitive relationship rows into the response envelope.
func BuildTransitiveRelationshipGraphResponse(metadataRow map[string]any, rows []map[string]any, direction string) map[string]any {
	response := cloneQueryAnyMap(metadataRow)
	response["outgoing"] = []map[string]any{}
	response["incoming"] = []map[string]any{}

	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		depth := querycontract.IntVal(row, "depth")
		if depth <= 0 {
			continue
		}
		if direction == "incoming" {
			sourceID := querycontract.StringVal(row, "source_id")
			sourceName := querycontract.StringVal(row, "source_name")
			key := fmt.Sprintf("incoming:%s:%s:%d", sourceID, sourceName, depth)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			response["incoming"] = append(response["incoming"].([]map[string]any), map[string]any{
				"direction":   "incoming",
				"type":        "CALLS",
				"source_name": sourceName,
				"source_id":   sourceID,
				"depth":       depth,
				"reason":      "transitive_call_graph",
			})
			continue
		}
		targetID := querycontract.StringVal(row, "target_id")
		targetName := querycontract.StringVal(row, "target_name")
		key := fmt.Sprintf("outgoing:%s:%s:%d", targetID, targetName, depth)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		response["outgoing"] = append(response["outgoing"].([]map[string]any), map[string]any{
			"direction":   "outgoing",
			"type":        "CALLS",
			"target_name": targetName,
			"target_id":   targetID,
			"depth":       depth,
			"reason":      "transitive_call_graph",
		})
	}

	return response
}

// mapRelationships is a family-local copy of root's code_relationships.go
// helper of the same name, with filterNullRelationships copied from root's
// infra_relationship_filter.go. The graph response normalizer shapes
// nullable relationship slices through them, and the staying
// relationship/infra readers that share them cannot cross the package
// boundary, so the leaf carries these byte-identical copies instead of
// importing root. Keep them behavior-identical to their root sources.
func mapRelationships(value any) []map[string]any {
	relationships, ok := value.([]map[string]any)
	if ok {
		return relationships
	}
	return filterNullRelationships(value)
}

// filterNullRelationships removes entries where type is nil (from OPTIONAL MATCH with no matches).
func filterNullRelationships(v any) []map[string]any {
	switch slice := v.(type) {
	case []map[string]any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			if item["type"] == nil {
				continue
			}
			result = append(result, item)
		}
		return result
	case []any:
		result := make([]map[string]any, 0, len(slice))
		for _, item := range slice {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Skip entries where type is nil (no relationship matched)
			if m["type"] == nil {
				continue
			}
			result = append(result, m)
		}
		return result
	default:
		return nil
	}
}

// dropNilOrEmptyRowKey is a family-local copy of root's
// code_relationship_story.go helper of the same name. The graph response
// normalizer omits null optional per-edge provenance fields through it,
// and the staying story shaper that shares it cannot cross the package
// boundary, so the leaf carries this byte-identical copy instead of
// importing root. Keep it behavior-identical to its root source.
func dropNilOrEmptyRowKey(row map[string]any, key string) {
	value, ok := row[key]
	if !ok {
		return
	}
	if value == nil {
		delete(row, key)
		return
	}
	if text, isString := value.(string); isString && strings.TrimSpace(text) == "" {
		delete(row, key)
	}
}

// NormalizeGraphRelationships normalizes the relationship slices of a graph response.
func NormalizeGraphRelationships(response map[string]any) {
	response["outgoing"] = normalizeGraphRelationshipSlice(mapRelationships(response["outgoing"]))
	response["incoming"] = normalizeGraphRelationshipSlice(mapRelationships(response["incoming"]))
}

func normalizeGraphRelationshipSlice(relationships []map[string]any) []map[string]any {
	if len(relationships) == 0 {
		return relationships
	}
	normalized := make([]map[string]any, 0, len(relationships))
	for _, relationship := range relationships {
		item := make(map[string]any, len(relationship)+1)
		for key, value := range relationship {
			item[key] = value
		}
		if querycontract.StringVal(item, "type") == "CALLS" && querycontract.StringVal(item, "call_kind") == "jsx_component" {
			item["type"] = "REFERENCES"
			if querycontract.StringVal(item, "reason") == "" {
				item["reason"] = "jsx_component_call_kind"
			}
		}
		dropNilOrEmptyRowKey(item, "confidence")
		dropNilOrEmptyRowKey(item, "resolution_method")
		normalized = append(normalized, item)
	}
	return normalized
}

// neo4jEntityUIDAnchorLabels is every label the graph schema gives a uid
// uniqueness constraint (uidConstraintLabels in go/internal/graph). Every
// writer that MERGEs one of these labels on uid also sets id to the same value
// or leaves id unset, so a uid seek over this set finds exactly the nodes the
// old (e.id = x OR e.uid = x) predicate found on these labels (issue #7057).
// TestNeo4jEntityIDAnchorLabelsMatchSchema fails when the schema list moves.
var neo4jEntityUIDAnchorLabels = []string{
	"AnalyticsModel", "Annotation", "ArgoCDApplication",
	"ArgoCDApplicationSet", "AtlantisProject", "AtlantisWorkflow",
	"CidrBlock", "Class", "CloudFormationCondition", "CloudFormationExport",
	"CloudFormationImport", "CloudFormationOutput", "CloudFormationParameter",
	"CloudFormationResource", "CloudResource", "CodeTaintEvidence",
	"Component", "ContainerImage", "ContainerImageDescriptor",
	"ContainerImageIndex", "ContainerImageTagObservation", "CrossplaneClaim",
	"CrossplaneComposition", "CrossplaneXRD", "DashboardAsset", "DataAsset",
	"DataColumn", "DataContract", "DataOwner", "DataQualityCheck", "Enum",
	"ExternalPrincipal", "File", "FluxBucket", "FluxGitRepository",
	"FluxHelmRelease", "FluxHelmRepository", "FluxKustomization",
	"FluxOCIRepository", "Function", "GitlabJob", "GitlabPipeline",
	"HelmChart", "HelmTemplateValueUsage", "HelmValueDefinition",
	"HelmValues", "ImplBlock", "IncidentRoutingEvidence", "Interface",
	"K8sResource", "KubernetesNamespace", "KubernetesWorkload",
	"KustomizeOverlay", "Macro", "Module", "OciImageDescriptor",
	"OciImageIndex", "OciImageManifest", "OciImageReferrer",
	"OciImageTagObservation", "OciRegistryRepository", "Package",
	"PackageArtifact", "PackageDependency", "PackageRegistryPackage",
	"PackageRegistryPackageArtifact", "PackageRegistryPackageDependency",
	"PackageRegistryPackageVersion", "PackageRegistryRegistryEvent",
	"PackageVersion", "PagerDutyDeclaration", "PrefixList", "Property",
	"Protocol", "ProtocolImplementation", "QueryExecution", "Record",
	"RegistryEvent", "SecretsIAMSecretMetadataPath",
	"SecretsIAMServiceAccount", "SecretsIAMVaultAuthRole",
	"SecretsIAMVaultPolicy", "SecurityGroupRule", "ShellCommand", "SqlColumn",
	"SqlFunction", "SqlIndex", "SqlMigration", "SqlTable", "SqlTrigger",
	"SqlView", "Struct", "TerraformBackend", "TerraformBlock",
	"TerraformCheck", "TerraformDataSource", "TerraformImport",
	"TerraformLocal", "TerraformLockProvider", "TerraformModule",
	"TerraformMovedBlock", "TerraformOutput", "TerraformProvider",
	"TerraformRemovedBlock", "TerraformResource", "TerraformStateResource",
	"TerraformVariable", "TerragruntConfig", "TerragruntDependency",
	"TerragruntInput", "TerragruntLocal", "Trait", "TypeAlias",
	"TypeAnnotation", "Typedef", "Union", "Variable",
}

// neo4jEntityIDAnchorLabels is every label the graph schema keys by an id
// uniqueness constraint. These nodes carry no uid (Repository, Workload and
// the workload-materializer labels MERGE on id), and resolve and repository
// reads hand their ids to the entity-id endpoints, so the anchor keeps an
// indexed id branch for them. TestNeo4jEntityIDAnchorLabelsMatchSchema pins
// this list to the schema.
var neo4jEntityIDAnchorLabels = []string{
	"CloudAction", "Endpoint", "EvidenceArtifact", "Platform", "Repository",
	"Workload", "WorkloadInstance",
}

// neo4jEntityUIDIndexAnchorLabels is every label MERGEd on uid that carries a
// uid RANGE index but no uid constraint (schema_tables_indexes.go). Rationale
// and DocumentationSection uids reach callers as the source_id of EXPLAINS and
// DOCUMENTS neighbours on the relationships row, so following one must resolve
// (#7057). TestNeo4jEntityIDAnchorLabelsMatchSchema pins this list to the
// schema, and TestNeo4jEntityIDAnchorCoversEveryUIDWriter fails when a writer
// MERGEs a uid-keyed label that none of the three lists covers.
var neo4jEntityUIDIndexAnchorLabels = []string{"DocumentationSection", "Rationale"}

var (
	neo4jEntityUIDAnchorDisjunction      = strings.Join(neo4jEntityUIDAnchorLabels, "|")
	neo4jEntityUIDIndexAnchorDisjunction = strings.Join(neo4jEntityUIDIndexAnchorLabels, "|")
	neo4jEntityIDAnchorDisjunction       = strings.Join(neo4jEntityIDAnchorLabels, "|")
)

// Neo4jEntityIDAnchor returns a scoped CALL () subquery that binds alias to
// the node whose uid (on a uid-constrained or uid-indexed label) or id (on an
// id-constrained label) equals param. It replaces the Neo4j use of
// GraphEntityIDPredicate as an anchor: that unlabeled
// (alias.id = x OR alias.uid = x) predicate cannot use any index and plans as
// an AllNodesScan, while each branch here plans as one index seek per label
// (issue #7057). UNION deduplicates a node more than one branch reaches. The
// empty variable scope clause needs Neo4j 5.23, the documented floor; bare
// CALL { } is deprecated from 5.23. Nodes with no uid or id at all
// (Parameter, Directory, name-keyed Module) were never matched by the old
// predicate either; see docs/internal/evidence/7057-relationship-uid-anchor.md.
// NornicDB readers keep their own label-resolved anchors and must not use this
// clause.
func Neo4jEntityIDAnchor(alias string, param string) string {
	return "CALL () {\n" +
		"\t\t\tMATCH (" + alias + ":" + neo4jEntityUIDAnchorDisjunction + " {uid: " + param + "})\n" +
		"\t\t\tRETURN " + alias + "\n" +
		"\t\t\tUNION\n" +
		"\t\t\tMATCH (" + alias + ":" + neo4jEntityUIDIndexAnchorDisjunction + " {uid: " + param + "})\n" +
		"\t\t\tRETURN " + alias + "\n" +
		"\t\t\tUNION\n" +
		"\t\t\tMATCH (" + alias + ":" + neo4jEntityIDAnchorDisjunction + " {id: " + param + "})\n" +
		"\t\t\tRETURN " + alias + "\n" +
		"\t\t}"
}
