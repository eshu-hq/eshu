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
	graphSchemaNeo4jFingerprint    = "ca479d532a310372af959c4fbabb17532d7c07e2d7342210b93283986beb07d2"
	graphSchemaNornicDBFingerprint = "3ffc1b84196c30fa96194c8f56cc53f63ca733008d6684492722dbc5ded2e9e3"
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
