package capabilitycatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// embeddedCatalog is the committed, generated catalog artifact. It is produced
// by cmd/capability-inventory and embedded so the API, MCP, and console can read
// the reconciled catalog at runtime without parsing specs or importing the MCP
// registry. A golden test keeps it in lockstep with the specs.
//
//go:embed data/catalog.generated.json
var embeddedCatalog []byte

// RawArtifact returns the committed catalog artifact bytes exactly as embedded.
// The generator's verify mode compares a fresh build against these bytes so the
// drift gate catches any deviation, including manual edits that the Catalog
// struct would otherwise drop on a round trip.
func RawArtifact() []byte {
	return embeddedCatalog
}

// Load returns the embedded, generated capability catalog. It is the runtime
// entry point for surfaces that serve catalog data.
func Load() (Catalog, error) {
	var catalog Catalog
	if err := json.Unmarshal(embeddedCatalog, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode embedded capability catalog: %w", err)
	}
	return catalog, nil
}

// BuildFromSpecs loads the matrix and overlay from specsDir and reconciles them
// with the supplied live signals. It is used by the generator and by drift
// tests; runtime callers should use Load instead.
func BuildFromSpecs(specsDir string, signals Signals) (Catalog, []Finding, error) {
	matrix, err := LoadMatrix(specsDir)
	if err != nil {
		return Catalog{}, nil, err
	}
	overlay, err := LoadOverlay(filepath.Join(specsDir, OverlayFileName))
	if err != nil {
		return Catalog{}, nil, err
	}
	authorization, err := LoadAuthorizationCatalog(filepath.Join(specsDir, AuthorizationFileName))
	if err != nil {
		return Catalog{}, nil, err
	}
	catalog, findings := BuildWithAuthorization(matrix, overlay, authorization, signals)
	return catalog, findings, nil
}

// MarshalCatalog renders the catalog as deterministic, indented JSON suitable
// for committing as the generated artifact. Entries are already sorted by id and
// Go marshals maps in sorted key order, so the output is stable.
func MarshalCatalog(catalog Catalog) ([]byte, error) {
	payload, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal capability catalog: %w", err)
	}
	return append(payload, '\n'), nil
}
