package terraformschema

import (
	"embed"
	"io/fs"
)

// embeddedSchemas carries every packaged Terraform provider schema bundle so
// runtime binaries (collector-terraform-state, future tooling) can resolve
// provider attribute coverage without depending on a source-tree path that
// only exists at build time.
//
// The relationships package already reads these schemas from disk during
// indexing and continues to do so via DefaultSchemaDir; embedded access is a
// second, container-safe entry point and is intentionally additive.
//
//go:embed schemas/*.json.gz
var embeddedSchemas embed.FS

// EmbeddedSchemasFS returns the embedded provider-schema bundle as an fs.FS
// rooted at the schemas directory. Callers can range fs.ReadDir to enumerate
// every "*.json.gz" file and open them with fs.ReadFile.
//
// This exists so runtime binaries can populate provider-attribute coverage
// without relying on a source-tree path. DefaultSchemaDir remains the
// canonical lookup for build-time tooling (Eshu indexing, fixture tests);
// EmbeddedSchemasFS is the container-safe fallback.
func EmbeddedSchemasFS() fs.FS {
	schemas, err := fs.Sub(embeddedSchemas, "schemas")
	if err != nil {
		// fs.Sub only fails when the directory name is invalid, which is
		// impossible for a constant string. Returning the root FS still lets
		// callers degrade gracefully rather than panic.
		return embeddedSchemas
	}
	return schemas
}
