// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SchemaApplication describes the checked-in graph schema contract that a
// runtime expects to write against.
type SchemaApplication struct {
	// Backend is the graph schema dialect covered by this application.
	Backend SchemaBackend
	// Fingerprint is the stable digest of backend, ordered schema DDL, and the
	// graph write-identity contract.
	Fingerprint string
	// StatementCount records the number of DDL statements included in the digest.
	StatementCount int
	// CompatibleFingerprints lists older writer fingerprints that may safely
	// write against this latest applied schema.
	CompatibleFingerprints []string
}

// graphWriteIdentityContract records the canonical MERGE identities graph
// writers key on that no DDL statement describes. It is hashed into the schema
// fingerprint so a writer whose identity contract disagrees with the applied
// one is refused before it writes.
//
// Why the digest needs this at all. The fingerprint decides whether an older
// writer may write against the applied schema, and a writer is unsafe when its
// write identity disagrees with the current one, not only when its DDL does.
// Issue #6102 is the case that proved it: moving the canonical import-graph
// Module key from {name} to {name, lang} changes no DDL, so an old pod computed
// the same digest, was admitted during a rolling upgrade, and its name-only
// `MATCH (m:Module {name: row.module_name})` bound both the Go and the Python
// node -- attaching a Go file's IMPORTS edge to the Python module. The edge
// survives the rollout: the canonical refresh only deletes IMPORTS edges for
// the file paths a generation projects, so a file that never changes again
// keeps it.
//
// Adding a line here is a fence, not a note. Change a canonical writer's
// MERGE/MATCH identity, add the line, and give the resulting fingerprint no
// compatible predecessors in graphSchemaCompatibleFingerprints.
var graphWriteIdentityContract = []string{
	// Canonical import-graph Module (#6102). See
	// canonicalNodeModuleUpsertCypher and canonicalNodeImportEdgeCypher in
	// go/internal/storage/cypher. Semantic Module entities are unaffected:
	// they MERGE on uid, which the uid uniqueness constraint already records
	// as DDL.
	"Module=name,lang",
}

// graphWriteIdentityDigestSection separates the write-identity contract from
// the DDL statements inside the fingerprint digest, so an identity line can
// never be mistaken for a schema statement and so introducing the section moves
// the digest on its own.
const graphWriteIdentityDigestSection = "graph-write-identity-contract"

const (
	// graphSchemaNeo4jFingerprint and graphSchemaNornicDBFingerprint are the
	// current schema digests. They now cover the graphWriteIdentityContract
	// section as well as the DDL, which is what moved them for the #6102 Module
	// (name, lang) identity cutover -- that change adds no DDL, so before the
	// section existed an older writer computed the identical digest and was
	// admitted.
	//
	// This bump is NOT additive, and it deliberately lists no compatible
	// predecessors (see graphSchemaCompatibleFingerprints). A writer on the
	// previous release resolves an import-edge target by module name alone, so
	// once both language nodes exist it binds the wrong one. It must stop
	// before writing, not write a graph an operator then has to repair.
	//
	// The DDL itself is byte-identical to the pre-cutover schema recorded in
	// graphSchemaNeo4jPreModuleIdentityFingerprint below, so bootstrap applies
	// exactly the same statements it did before.
	graphSchemaNeo4jFingerprint    = "fb55804c8e91a393be08c56f4c637fe1171d8c82e23fa24135eb543c92c19838"
	graphSchemaNornicDBFingerprint = "27e278562803233a078fc381d92c27223f940325cc5770cfd303fa852cff8a3f"

	// graphSchemaNeo4jPreModuleIdentityFingerprint and its NornicDB peer are the
	// digests an older writer computes: the same DDL, hashed before the
	// write-identity contract joined the digest. They are recorded so the
	// compatibility gate can prove such a writer is refused, and they are
	// deliberately absent from the current fingerprint's compatible list.
	//
	// This schema carries the #5458 RegistryEvent /
	// PackageRegistryRegistryEvent uid uniqueness constraints
	// (uidConstraintLabels). A #5820 P2 review found no query anywhere in the
	// repo filtering PackageArtifact by version_id or package_id, and the same
	// repo-wide search found none filtering RegistryEvent by those properties
	// either -- both deferred edge MATCHes (HAS_ARTIFACT in
	// package_registry_artifact_writer.go, HAS_REGISTRY_EVENT in
	// package_registry_event_writer.go) anchor on uid only -- so this schema
	// never carried a registry_event_version_id/registry_event_package_id
	// lookup index pair; only the uid constraint was added
	// (schema_tables_indexes.go's NOTE documents the same decision for
	// PackageArtifact). That bump was additive: a writer running its
	// predecessor creates no RegistryEvent nodes, so the constraint never
	// applies to it, and the predecessor
	// (graphSchemaNeo4jPreRegistryEventFingerprint /
	// graphSchemaNornicDBPreRegistryEventFingerprint, the #5458 PackageArtifact
	// tip below) stayed compatible with it.
	graphSchemaNeo4jPreModuleIdentityFingerprint    = "8cb1b9c85e5e60690f69af2f35b227af357474845dcec947c8e53be66a1d2647"
	graphSchemaNornicDBPreModuleIdentityFingerprint = "d2f02330a087a4ece09cb9f81505909b1afbff48719e056c461ae776ceacd9bc"

	// graphSchemaNeo4jPreRegistryEventFingerprint and its NornicDB peer are the
	// schema fingerprints immediately before the #5458 RegistryEvent /
	// PackageRegistryRegistryEvent uid constraint was added (the registry_event
	// slice of the #5458 orphan-kind epic, landing after the package_artifact
	// slice below). These equal graphSchemaNeo4jFingerprint /
	// graphSchemaNornicDBFingerprint's value before this addition -- the #5820
	// P2-fixed PackageArtifact tip (dead lookup indexes already removed). The
	// bump is additive: a writer running the predecessor schema creates no
	// RegistryEvent nodes, so the new constraint never applies to it, and the
	// predecessor (graphSchemaNeo4jPreArtifactFingerprint /
	// graphSchemaNornicDBPreArtifactFingerprint) stays compatible.
	graphSchemaNeo4jPreRegistryEventFingerprint    = "d7985da368fd30df0e0bfcf86a2f14cc588b7ba649f555d36927984ed749a9f9"
	graphSchemaNornicDBPreRegistryEventFingerprint = "89a8a9239e99b3e7c808148d120986f09bc9e242333718b07bd5efe18e192f70"

	// graphSchemaNeo4jPreArtifactFingerprint and its NornicDB peer are the
	// schema fingerprints immediately before the #5458 PackageArtifact /
	// PackageRegistryPackageArtifact uid constraints and lookup indexes were
	// added. These equal graphSchemaNeo4jFingerprint /
	// graphSchemaNornicDBFingerprint's value before this addition (the merged
	// #5651 KubernetesNamespace tip below).
	graphSchemaNeo4jPreArtifactFingerprint    = "b54c586015a30b929b103723c5549e424d800d1159253e8f4745d90af24ba94b"
	graphSchemaNornicDBPreArtifactFingerprint = "ddaa10e5b634a4c42796ba01d2f8dd88181f93a4c0a73655d4cae6233f4e0a2e"

	// graphSchemaNeo4jPreKubernetesNamespaceIndexesFingerprint and its NornicDB
	// peer are the schema fingerprints immediately before the #5651
	// KubernetesNamespace uid constraint and cluster_id/namespace read indexes
	// were added. These equal graphSchemaNeo4jFingerprint /
	// graphSchemaNornicDBFingerprint's value before this addition (the merged
	// #5445 kustomize_overlay_repo_id tip below).
	graphSchemaNeo4jPreKubernetesNamespaceIndexesFingerprint    = "2e67a8b4e803a76934025267f5b8ff750a021dbc737c250909fd50033fd8bfef"
	graphSchemaNornicDBPreKubernetesNamespaceIndexesFingerprint = "24ca51d4d323ac10d426ee75defb32fa85c51c471f831c7058eaf909f40c2891"

	// graphSchemaNeo4jPreKustomizeOverlayRepoIDIndexFingerprint and its
	// NornicDB peer are the schema fingerprints immediately before the #5445
	// kustomize_overlay_repo_id index was added. The bump is additive: a
	// writer running the predecessor schema still writes and reads correctly,
	// it only lacks the faster repo-scoped KustomizeOverlay lookup the #5445
	// EXTENDS_BASE resolver needs until its own schema catches up, so the
	// predecessor stays compatible. These equal graphSchemaNeo4jFingerprint /
	// graphSchemaNornicDBFingerprint's value before this addition, which was
	// the merged schema described immediately below (CodeownerTeam.ref,
	// KubernetesWorkload.id lookup, and the #5443 TerraformStateResource
	// bumps).
	graphSchemaNeo4jPreKustomizeOverlayRepoIDIndexFingerprint    = "764422ab57449b6c2671eb60c15c8a957607d6f27c47c1100fc5e9ce4e12b582"
	graphSchemaNornicDBPreKustomizeOverlayRepoIDIndexFingerprint = "50844cbba41348beebe489ead92a498ac59b1b34842edfd5dbd2fa028bf1319d"

	// graphSchemaNeo4jPreTerraformStateResourceAddressIndexFingerprint and its
	// NornicDB peer are the schema fingerprints immediately before the second
	// #5443 P1 review finding's fix (the tf_state_resource_address
	// TerraformStateResource property index). The bump is additive: it is an
	// index-only addition -- a writer running the predecessor schema still
	// writes and reads correctly, it only lacks the faster
	// TerraformStateResource address lookup path until its own schema catches
	// up, so the predecessor stays compatible. These equal
	// graphSchemaNeo4jFingerprint / graphSchemaNornicDBFingerprint's value
	// before this addition.
	graphSchemaNeo4jPreTerraformStateResourceAddressIndexFingerprint    = "6bd8a2e1e39b53156bf56e6e693eb2f5b57e7528d2355460b95286ff38de0190"
	graphSchemaNornicDBPreTerraformStateResourceAddressIndexFingerprint = "2e37a54ed0627eac8bb495550d4c566b66811a75156168e6a3039e8ee6c13384"

	// graphSchemaNeo4jPreTerraformStateResourceIndexesFingerprint and its
	// NornicDB peer are the schema fingerprints immediately before the first
	// #5443 P1 review finding's fix (tf_state_resource_type /
	// tf_state_resource_name TerraformStateResource property indexes, and
	// TerraformStateResource joining the infra_search_index fulltext label
	// set). The bump is additive: both are index-only additions -- a writer
	// running the predecessor schema still writes and reads correctly, it
	// only lacks the faster TerraformStateResource lookup path and the
	// fulltext label until its own schema catches up, so the predecessor
	// stays compatible.
	graphSchemaNeo4jPreTerraformStateResourceIndexesFingerprint    = "705027c6a334f58378432a69f8bbccc02f8533707bf2b94f7a8a80bb57acb2df"
	graphSchemaNornicDBPreTerraformStateResourceIndexesFingerprint = "0d8bf637854c05e1ddfe4ce8a8e73f38970e974fd2f97318d03876fa7b3e3a9b"

	// graphSchemaNeo4jPreCodeownersOwnershipFingerprint and its NornicDB peer
	// are the schema fingerprints immediately before the CodeownerTeam.ref
	// uniqueness constraint and NornicDB lookup index were added (issue #5419
	// Phase 3, the DECLARES_CODEOWNER edge target). The bump is additive: a
	// writer running the predecessor schema creates no CodeownerTeam nodes, so
	// the new constraint never applies to it.
	graphSchemaNeo4jPreCodeownersOwnershipFingerprint    = "ad2a8291d1aa3766839c46d708f3641a1ec7c6fc0d2126de1c901f5b1997ebd7"
	graphSchemaNornicDBPreCodeownersOwnershipFingerprint = "9b67c40d329b0309bb1247cf86c1f0574f9ddf31b8e6ab47de9416e960af0b70"

	// graphSchemaNornicDBPreKubernetesWorkloadIDLookupFingerprint is the schema
	// fingerprint immediately before the NornicDB-only KubernetesWorkload.id
	// lookup index was added (#5436), giving the RUNS_IMAGE edge read path a
	// seek-consistent anchor to match the other by-id-anchored labels
	// analyze_infra_relationships serves. The bump is additive and NornicDB-only
	// (Neo4j's fingerprint is unaffected): older writers do not rely on the
	// index being absent, and newer writers only gain a faster
	// KubernetesWorkload lookup instead of falling back to a label scan.
	graphSchemaNornicDBPreKubernetesWorkloadIDLookupFingerprint = "9b67c40d329b0309bb1247cf86c1f0574f9ddf31b8e6ab47de9416e960af0b70"

	// graphSchemaNeo4jPreTerraformStateResourceSplitFingerprint and its
	// NornicDB peer are the schema fingerprints immediately before the
	// TerraformStateResource uid uniqueness constraint was added (#5443:
	// Terraform-state-observed resources now MERGE under their own label
	// instead of sharing TerraformResource with config-declared resources).
	// The immediate predecessor is the #5419/#5436 merged schema above (the
	// bump lands after both), so these equal graphSchemaNeo4jFingerprint /
	// graphSchemaNornicDBFingerprint's value before this addition. The bump is
	// additive: a writer running the predecessor schema creates no
	// TerraformStateResource nodes (it still writes the old TerraformResource
	// label), so the predecessor stays compatible, and the additive chain
	// pre-CodeownersOwnership -> ... -> this bump is cumulative, so every
	// earlier predecessor below stays compatible too.
	graphSchemaNeo4jPreTerraformStateResourceSplitFingerprint    = "f69cb50986b83d379d7372b4ea9bcbc488d93b2b520d2dd8f67aea91ee381baf"
	graphSchemaNornicDBPreTerraformStateResourceSplitFingerprint = "be6a2e36e20dd5b234332c39e723e11f3374990191f62d7fa5e514487720d1c7"

	// graphSchemaNeo4jPreFluxHelmEntitiesFingerprint and its NornicDB peer are
	// the schema fingerprints immediately before the FluxHelmRelease /
	// FluxHelmRepository uid uniqueness constraints were added (issue #5483
	// C1). On the current history the immediately-preceding schema is the
	// #5360 PR A Flux typed-entities bump, so these equal that predecessor's
	// (then-current) fingerprints -- the values graphSchemaNeo4jFingerprint/
	// graphSchemaNornicDBFingerprint held before this change. The bump is
	// additive: a writer running the predecessor schema creates no
	// FluxHelmRelease/FluxHelmRepository nodes, so the predecessor stays
	// compatible.
	graphSchemaNeo4jPreFluxHelmEntitiesFingerprint    = "edf86cd974966f8ddf66d050185f0f8ebeb3155b2106bfa7484a63d865699108"
	graphSchemaNornicDBPreFluxHelmEntitiesFingerprint = "c5f668561275341825c53914e7f92cc10ad54bdf229eae143cd8f7c8c153c8ba"

	// graphSchemaNeo4jPreFluxTypedEntitiesFingerprint and its NornicDB peer are
	// the schema fingerprints immediately before the FluxKustomization /
	// FluxGitRepository / FluxOCIRepository / FluxBucket uid uniqueness
	// constraints were added (issue #5360 PR A). On the current history the
	// immediately-preceding schema is the SqlMigration bump (#5346), so these
	// equal that predecessor's fingerprints. The bump is additive: a writer
	// running the predecessor schema creates no Flux typed-entity nodes, so the
	// predecessor stays compatible.
	graphSchemaNeo4jPreFluxTypedEntitiesFingerprint    = "2a84ae8521f4930e8ce3ba8ff7556ea2fb53b5cb843a60f1aab1b169e50bfda0"
	graphSchemaNornicDBPreFluxTypedEntitiesFingerprint = "35725c2a4d5a07e2fbeaddc5399a6e20af438a0193c4ebc8c1ecddacbf8b5866"

	// graphSchemaNornicDBPreFunctionLegacyIDLookupFingerprint is the schema
	// immediately before the additive Function.id lookup used by the bounded
	// relationship-story legacy fallback. Older writers do not depend on this
	// secondary index being absent.
	graphSchemaNornicDBPreFunctionLegacyIDLookupFingerprint = "1c4bf2acf328fdeb19084b18618cc9a57749615d7c513edb674cfbc036f1bbae"

	// graphSchemaNeo4jPreShellExecRetractIndexesFingerprint and its NornicDB
	// peer are the schema fingerprints immediately before ShellCommand repo_id/path
	// lookup indexes were added. The bump is additive: older writers do not rely
	// on the indexes being absent, and newer writers only gain faster
	// reducer-owned shell_exec edge retractions.
	graphSchemaNeo4jPreShellExecRetractIndexesFingerprint    = "489250e081f0328b36cc7eb4fd21d25eb789b17e63ea64341e678f00be681ecd"
	graphSchemaNornicDBPreShellExecRetractIndexesFingerprint = "14acea00f37e5c8ad971662dde5fbebddffc6eab8a6d2cd7544c2f966a10c054"

	// graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint and its NornicDB
	// peer are the schema fingerprints immediately before inheritance child
	// repo_id/path lookup indexes were added. The bump is additive: older
	// writers do not rely on the indexes being absent, and newer writers only
	// gain faster reducer-owned inheritance edge retractions.
	graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint    = "3a34d8460063f6d6e390dbea3bdacd1ecf0f2e9ff8b92bbea0b7382f1fdf2246"
	graphSchemaNornicDBPreInheritanceRetractIndexesFingerprint = "2e29b77ef4364aa4653ad1d6398cee136e3c4c099e2f2eb157eae38a1f10b377"

	// graphSchemaNeo4jPreFunctionRetractIndexesFingerprint and its NornicDB peer
	// are the schema fingerprints immediately before the Function repo_id/path
	// lookup indexes were added. The bump is additive: older writers do not rely
	// on the indexes being absent, and newer writers only gain faster
	// reducer-owned Function edge retractions.
	graphSchemaNeo4jPreFunctionRetractIndexesFingerprint    = "e5d984d669fe8fd4917e2f279fd2ebc5259a3f0a30e4811ee29f8a2767dc2c7b"
	graphSchemaNornicDBPreFunctionRetractIndexesFingerprint = "bbe407e1d1bd45cb80c93cb0f6768d4e078916bb0e1226ffea51e5ecaa6e644a"

	// graphSchemaNeo4jPreHelmTemplateValuesFingerprint and its NornicDB peer are
	// the schema fingerprints immediately before the Helm template-value bump
	// (adding HelmValueDefinition/HelmTemplateValueUsage uniqueness + uid
	// constraints). That bump is additive — a writer running the predecessor
	// schema creates no Helm template-value nodes, so the new constraints never
	// apply to it — so the predecessor is recorded as compatible to avoid a
	// needless incompatible-schema path during a rolling deploy. These
	// predecessors are the GitLab-bump fingerprints (the prior current schema).
	graphSchemaNeo4jPreHelmTemplateValuesFingerprint    = "5c03985679793d71accf72f200386ce42c44d6876ee11b9aa4911f1f3c0f67fd"
	graphSchemaNornicDBPreHelmTemplateValuesFingerprint = "96e23958aed519860d44bdabf0e45d9f864c94a76ca6da1e002664892e4b46f1"

	// graphSchemaNeo4jPreGitlabFingerprint and its NornicDB peer are the schema
	// fingerprints from before the GitLab bump — the additive predecessor of the
	// PreHelmTemplateValues (GitLab) schema. The additive chain
	// pre-GitLab -> GitLab -> Helm-template-values is cumulative: a writer on the
	// pre-GitLab schema creates neither GitLab nor Helm template-value nodes, so
	// it stays compatible with the current Helm schema and must remain in the
	// compatible list (dropping it would needlessly reject a pre-GitLab writer
	// during a rolling deploy).
	graphSchemaNeo4jPreGitlabFingerprint    = "be5aa2ca69761b9db112d7a45487ef7095b3fd58038de17cb2b3047479b93c0e"
	graphSchemaNornicDBPreGitlabFingerprint = "b9e6a46df32f87a20b85cc5e8864a5b70bf0aa478edb055d17fc35d50204c3ff"

	// graphSchemaNeo4jPreContentEntityGraphFingerprint and its NornicDB peer are
	// the fingerprints from before the #5954 bump, which adds uid uniqueness
	// constraints for TerraformBlock, CloudFormationCondition,
	// CloudFormationImport, CloudFormationExport and PagerDutyDeclaration so those
	// content-entity labels get the same MERGE-by-uid index every sibling label
	// has.
	//
	// Additive, and compatible in both directions during a rolling deploy: a
	// writer on the predecessor schema creates none of these five node types (the
	// projector did not recognise their entity types at all), so it cannot violate
	// a constraint that only now exists, and a writer on the new schema adds
	// constraints an older reader simply does not consult.
	//
	// Registered in uidConstraintLabels rather than as composite
	// (name, path, line_number) entries in schemaConstraints: composite
	// constraints pass through dialect.constraint(), which drops them for
	// NornicDB. Both backend fingerprints moving here is the evidence the
	// constraint actually lands on both.
	graphSchemaNeo4jPreContentEntityGraphFingerprint    = "2deeea50c722b1ac6be0d86421a450102ff2b8f057142b37277620dd43642f40"
	graphSchemaNornicDBPreContentEntityGraphFingerprint = "f22de603056a95f35a93db7ba8498949ad37670c44d2c3623daf3527b1ac27ae"

	// graphSchemaNeo4jPreSqlMigrationFingerprint and its NornicDB peer are the
	// schema fingerprints immediately before the SqlMigration bump (#5346: adds
	// the SqlMigration uid uniqueness constraint so the new content-entity label
	// gets the same MERGE-by-uid index every other SQL entity label has). The
	// bump is additive: a writer running the predecessor schema creates no
	// SqlMigration nodes, so the new constraint never applies to it, and the
	// additive chain pre-Helm-template-values -> Helm-template-values ->
	// SqlMigration is cumulative, so the prior current (Helm) schema stays
	// compatible too.
	graphSchemaNeo4jPreSqlMigrationFingerprint    = "556d133c15610ecaaf773af2200717062e5d91d0edd2709fa7f6a83072a11c53"
	graphSchemaNornicDBPreSqlMigrationFingerprint = "cfff663a3a7cae4e7c36823e0304b25f7f046eed2e139951e8a9bf8121b9ba69"
)

// graphSchemaPreModuleIdentityFingerprints maps a backend to the digest an
// older writer computes immediately before the #6102 Module (name, lang)
// identity cutover. PreModuleIdentitySchemaApplication reads it.
var graphSchemaPreModuleIdentityFingerprints = map[SchemaBackend]string{
	SchemaBackendNeo4j:    graphSchemaNeo4jPreModuleIdentityFingerprint,
	SchemaBackendNornicDB: graphSchemaNornicDBPreModuleIdentityFingerprint,
}

// graphSchemaCompatibleFingerprints lists additive predecessor schema
// fingerprints that older graph writers may safely use after bootstrap records
// the current marker. The key is the schema fingerprint that was applied; the
// value lists predecessor fingerprints whose writers stay compatible with it.
// Destructive schema changes, schema changes coupled to new reducer domains,
// and write-identity cutovers must not list predecessors.
//
// The current fingerprint maps to an empty list on purpose: the #6102 Module
// (name, lang) cutover changes what a writer MERGEs on, and a writer on any
// earlier release resolves an import-edge target by module name alone. The
// pre-cutover entry below is retained, keyed by that schema's own fingerprint
// rather than the current one, so it can never be reached by the current
// lookup. It records what that schema admitted, and the fence tests drive the
// real admission decision with it.
var graphSchemaCompatibleFingerprints = map[SchemaBackend]map[string][]string{
	SchemaBackendNeo4j: {
		graphSchemaNeo4jFingerprint: {},
		graphSchemaNeo4jPreModuleIdentityFingerprint: {
			graphSchemaNeo4jPreRegistryEventFingerprint,
			graphSchemaNeo4jPreArtifactFingerprint,
			graphSchemaNeo4jPreKubernetesNamespaceIndexesFingerprint,
			graphSchemaNeo4jPreKustomizeOverlayRepoIDIndexFingerprint,
			graphSchemaNeo4jPreTerraformStateResourceAddressIndexFingerprint,
			graphSchemaNeo4jPreTerraformStateResourceIndexesFingerprint,
			graphSchemaNeo4jPreTerraformStateResourceSplitFingerprint,
			graphSchemaNeo4jPreCodeownersOwnershipFingerprint,
			graphSchemaNeo4jPreFluxHelmEntitiesFingerprint,
			graphSchemaNeo4jPreFluxTypedEntitiesFingerprint,
			graphSchemaNeo4jPreSqlMigrationFingerprint,
			graphSchemaNeo4jPreShellExecRetractIndexesFingerprint,
			graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint,
			graphSchemaNeo4jPreFunctionRetractIndexesFingerprint,
			graphSchemaNeo4jPreHelmTemplateValuesFingerprint,
			graphSchemaNeo4jPreGitlabFingerprint,
			graphSchemaNeo4jPreContentEntityGraphFingerprint,
		},
	},
	SchemaBackendNornicDB: {
		graphSchemaNornicDBFingerprint: {},
		graphSchemaNornicDBPreModuleIdentityFingerprint: {
			graphSchemaNornicDBPreRegistryEventFingerprint,
			graphSchemaNornicDBPreArtifactFingerprint,
			graphSchemaNornicDBPreKubernetesNamespaceIndexesFingerprint,
			graphSchemaNornicDBPreKustomizeOverlayRepoIDIndexFingerprint,
			graphSchemaNornicDBPreTerraformStateResourceAddressIndexFingerprint,
			graphSchemaNornicDBPreTerraformStateResourceIndexesFingerprint,
			graphSchemaNornicDBPreTerraformStateResourceSplitFingerprint,
			graphSchemaNornicDBPreCodeownersOwnershipFingerprint,
			graphSchemaNornicDBPreKubernetesWorkloadIDLookupFingerprint,
			graphSchemaNornicDBPreFluxHelmEntitiesFingerprint,
			graphSchemaNornicDBPreFluxTypedEntitiesFingerprint,
			graphSchemaNornicDBPreSqlMigrationFingerprint,
			graphSchemaNornicDBPreFunctionLegacyIDLookupFingerprint,
			graphSchemaNornicDBPreShellExecRetractIndexesFingerprint,
			graphSchemaNornicDBPreInheritanceRetractIndexesFingerprint,
			graphSchemaNornicDBPreFunctionRetractIndexesFingerprint,
			graphSchemaNornicDBPreHelmTemplateValuesFingerprint,
			graphSchemaNornicDBPreGitlabFingerprint,
			graphSchemaNornicDBPreContentEntityGraphFingerprint,
		},
	},
}

// SchemaApplicationForBackend returns the graph schema fingerprint and
// compatibility set for backend.
func SchemaApplicationForBackend(backend SchemaBackend) (SchemaApplication, error) {
	statements, err := SchemaStatementsForBackend(backend)
	if err != nil {
		return SchemaApplication{}, err
	}

	fingerprint := graphSchemaFingerprint(backend, statements, graphWriteIdentityContract)
	compatible := append([]string(nil), graphSchemaCompatibleFingerprints[backend][fingerprint]...)
	if compatible == nil {
		compatible = []string{}
	}
	return SchemaApplication{
		Backend:                backend,
		Fingerprint:            fingerprint,
		StatementCount:         len(statements),
		CompatibleFingerprints: compatible,
	}, nil
}

// graphSchemaFingerprint digests backend, the ordered DDL statements, and the
// write-identity contract into the value RequireCompatible compares. It takes
// the identity contract as an argument so a test can digest a historical
// contract without mutating package state.
func graphSchemaFingerprint(backend SchemaBackend, statements, identities []string) string {
	hasher := sha256.New()
	_, _ = hasher.Write([]byte(string(backend)))
	_, _ = hasher.Write([]byte{0})
	for _, statement := range statements {
		_, _ = hasher.Write([]byte(statement))
		_, _ = hasher.Write([]byte{0})
	}
	_, _ = hasher.Write([]byte(graphWriteIdentityDigestSection))
	_, _ = hasher.Write([]byte{0})
	for _, identity := range identities {
		_, _ = hasher.Write([]byte(identity))
		_, _ = hasher.Write([]byte{0})
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// PreModuleIdentitySchemaApplication returns the schema application an older
// writer computes immediately before the #6102 Module (name, lang) identity
// cutover: the fingerprint that writer expects, and the predecessors that
// schema admitted.
//
// It exists so the compatibility gate can prove -- against the real admission
// decision rather than a restatement of it -- that such a writer is refused
// once the cutover schema is applied. Intended for tests and static contract
// checks only, like MustSchemaApplicationForBackend.
func PreModuleIdentitySchemaApplication(backend SchemaBackend) (SchemaApplication, error) {
	statements, err := SchemaStatementsForBackend(backend)
	if err != nil {
		return SchemaApplication{}, err
	}
	fingerprint, ok := graphSchemaPreModuleIdentityFingerprints[backend]
	if !ok {
		return SchemaApplication{}, fmt.Errorf("no pre-module-identity fingerprint for backend %s", backend)
	}
	compatible := append([]string(nil), graphSchemaCompatibleFingerprints[backend][fingerprint]...)
	if compatible == nil {
		compatible = []string{}
	}
	return SchemaApplication{
		Backend:                backend,
		Fingerprint:            fingerprint,
		StatementCount:         len(statements),
		CompatibleFingerprints: compatible,
	}, nil
}

// MustSchemaApplicationForBackend returns the schema application or panics.
// It is intended only for package-level tests and static contract checks.
func MustSchemaApplicationForBackend(backend SchemaBackend) SchemaApplication {
	app, err := SchemaApplicationForBackend(backend)
	if err != nil {
		panic(fmt.Sprintf("graph schema application for %s: %v", backend, err))
	}
	return app
}
