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
	graphSchemaNeo4jFingerprint    = "e5588d8865f16cbed65dcbb1eece06822b270713c12b9def4748b5f26a0894c8"
	graphSchemaNornicDBFingerprint = "6301b920bd152e7a52918dc75ad26557233631f225e8efc295cc536b28094d87"
)

// graphSchemaCompatibleFingerprints lists additive predecessor schema
// fingerprints that older graph writers may safely use after bootstrap records
// the current marker. Destructive schema changes and schema changes coupled to
// new reducer domains must not list predecessors.
var graphSchemaCompatibleFingerprints = map[SchemaBackend]map[string][]string{}

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
