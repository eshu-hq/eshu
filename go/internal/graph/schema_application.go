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
	graphSchemaNeo4jFingerprint    = "556d133c15610ecaaf773af2200717062e5d91d0edd2709fa7f6a83072a11c53"
	graphSchemaNornicDBFingerprint = "cfff663a3a7cae4e7c36823e0304b25f7f046eed2e139951e8a9bf8121b9ba69"

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
			graphSchemaNeo4jPreShellExecRetractIndexesFingerprint,
			graphSchemaNeo4jPreInheritanceRetractIndexesFingerprint,
			graphSchemaNeo4jPreFunctionRetractIndexesFingerprint,
			graphSchemaNeo4jPreHelmTemplateValuesFingerprint,
			graphSchemaNeo4jPreGitlabFingerprint,
		},
	},
	SchemaBackendNornicDB: {
		graphSchemaNornicDBFingerprint: {
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
