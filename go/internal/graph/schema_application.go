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
	// Fingerprint is the stable digest of backend plus ordered schema DDL.
	Fingerprint string
	// StatementCount records the number of DDL statements included in the digest.
	StatementCount int
	// CompatibleFingerprints lists older writer fingerprints that may safely
	// write against this latest applied schema.
	CompatibleFingerprints []string
}

const (
	// graphSchemaNeo4jFingerprint and graphSchemaNornicDBFingerprint are the
	// current schema digests for the merged schema (both the CodeownerTeam.ref
	// constraint/index from #5419 and the KubernetesWorkload.id lookup index
	// from #5436 present). The KubernetesWorkload.id lookup index is
	// NornicDB-only, so the Neo4j digest is unchanged from #5419's value; the
	// NornicDB digest reflects both additions.
	graphSchemaNeo4jFingerprint    = "f69cb50986b83d379d7372b4ea9bcbc488d93b2b520d2dd8f67aea91ee381baf"
	graphSchemaNornicDBFingerprint = "be6a2e36e20dd5b234332c39e723e11f3374990191f62d7fa5e514487720d1c7"

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

// graphSchemaCompatibleFingerprints lists additive predecessor schema
// fingerprints that older graph writers may safely use after bootstrap records
// the current marker. The key is the current (latest) schema fingerprint; the
// value lists predecessor fingerprints whose writers stay compatible.
// Destructive schema changes and schema changes coupled to new reducer domains
// must not list predecessors.
var graphSchemaCompatibleFingerprints = map[SchemaBackend]map[string][]string{
	SchemaBackendNeo4j: {
		graphSchemaNeo4jFingerprint: {
			graphSchemaNeo4jPreCodeownersOwnershipFingerprint,
			graphSchemaNeo4jPreFluxHelmEntitiesFingerprint,
			graphSchemaNeo4jPreFluxTypedEntitiesFingerprint,
			graphSchemaNeo4jPreSqlMigrationFingerprint,
			graphSchemaNeo4jPreShellExecRetractIndexesFingerprint,
			graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint,
			graphSchemaNeo4jPreFunctionRetractIndexesFingerprint,
			graphSchemaNeo4jPreHelmTemplateValuesFingerprint,
			graphSchemaNeo4jPreGitlabFingerprint,
		},
	},
	SchemaBackendNornicDB: {
		graphSchemaNornicDBFingerprint: {
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

	hasher := sha256.New()
	_, _ = hasher.Write([]byte(string(backend)))
	_, _ = hasher.Write([]byte{0})
	for _, statement := range statements {
		_, _ = hasher.Write([]byte(statement))
		_, _ = hasher.Write([]byte{0})
	}

	fingerprint := hex.EncodeToString(hasher.Sum(nil))
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
