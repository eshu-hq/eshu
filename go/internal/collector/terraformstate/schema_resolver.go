package terraformstate

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/terraformschema"
)

// packagedSchemaResolver is the production ProviderSchemaResolver. It is
// populated once at collector startup from the gzipped JSON bundles shipped
// in go/internal/terraformschema/schemas/ and answers HasAttribute lookups
// without I/O on the parse path.
//
// The map is built once and treated as immutable; concurrent HasAttribute
// callers therefore need no synchronization.
type packagedSchemaResolver struct {
	resourceAttributes map[string]map[string]struct{}
}

// HasAttribute returns true when the loaded provider schemas declare an
// attribute or block_type named attributeKey on the resourceType. Both
// arguments are matched as-is; callers must pass the verbatim Terraform
// resource type (e.g. "aws_s3_bucket") and key (e.g. "acl" or
// "server_side_encryption_configuration").
func (r *packagedSchemaResolver) HasAttribute(resourceType string, attributeKey string) bool {
	if r == nil {
		return false
	}
	attrs, ok := r.resourceAttributes[resourceType]
	if !ok {
		return false
	}
	_, ok = attrs[attributeKey]
	return ok
}

// LoadPackagedSchemaResolver builds a ProviderSchemaResolver from the gzipped
// JSON provider schemas. It first scans schemaDir on disk; when schemaDir is
// blank, missing, or contains no parseable bundles, it falls back to the
// schemas embedded inside the binary via terraformschema.EmbeddedSchemasFS so
// containerized binaries continue to resolve attribute coverage without
// shipping the schemas directory.
//
// Both sources merge each provider's resource_schemas — top-level attributes
// and block_types — into a flat resourceType -> trusted-key lookup. Terraform
// state JSON stores nested blocks (versioning,
// server_side_encryption_configuration, lifecycle_rule, ...) under the same
// `attributes` key as scalar attributes, so the trusted surface must include
// both names.
//
// Returns nil when neither source yields any parseable schema. Returning nil
// is intentional: the parser treats a nil resolver as redact.SchemaUnknown
// which fails closed under the configured RedactionRules.
func LoadPackagedSchemaResolver(schemaDir string) (ProviderSchemaResolver, error) {
	resourceAttributes := make(map[string]map[string]struct{}, 1024)

	if schemaDir != "" {
		matches, err := filepath.Glob(filepath.Join(schemaDir, "*.json*"))
		if err != nil {
			return nil, fmt.Errorf("scan terraform schemas in %q: %w", schemaDir, err)
		}
		sort.Strings(matches)
		for _, schemaPath := range matches {
			file, err := os.Open(schemaPath)
			if err != nil {
				continue
			}
			parseSchemaInto(resourceAttributes, file, schemaPath)
			_ = file.Close()
		}
	}

	if len(resourceAttributes) == 0 {
		if err := loadEmbeddedSchemasInto(resourceAttributes); err != nil {
			return nil, err
		}
	}

	if len(resourceAttributes) == 0 {
		return nil, nil
	}
	return &packagedSchemaResolver{resourceAttributes: resourceAttributes}, nil
}

// loadEmbeddedSchemasInto walks the embedded provider-schema bundle and
// merges every parseable bundle into dst. Embedded reads cannot fail for
// missing files (the bundle is fixed at build time), so this only returns
// errors for genuine filesystem walk failures from fs.WalkDir.
func loadEmbeddedSchemasInto(dst map[string]map[string]struct{}) error {
	embedded := terraformschema.EmbeddedSchemasFS()
	entries, err := fs.ReadDir(embedded, ".")
	if err != nil {
		return fmt.Errorf("read embedded terraform schemas: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json.gz") && !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	for _, name := range names {
		file, err := embedded.Open(name)
		if err != nil {
			continue
		}
		parseSchemaInto(dst, file, name)
		_ = file.Close()
	}
	return nil
}

// parseSchemaInto decodes one gzipped or plain JSON provider schema bundle
// and folds it into dst. Errors and malformed bundles silently skip so one
// bad file cannot prevent the resolver from honoring the rest of the
// packaged schemas.
func parseSchemaInto(dst map[string]map[string]struct{}, file io.Reader, name string) {
	reader := file
	if strings.HasSuffix(name, ".gz") {
		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		reader = gzipReader
	}

	var doc schemaDocument
	if err := json.NewDecoder(reader).Decode(&doc); err != nil {
		return
	}
	for _, provider := range doc.ProviderSchemas {
		for resourceType, resource := range provider.ResourceSchemas {
			mergeResourceBlock(dst, resourceType, resource.Block)
		}
	}
}

// schemaDocument mirrors the structure of `terraform providers schema -json`
// output. It is duplicated locally instead of imported from terraformschema
// so the resolver can fold block_type names into the trusted surface; the
// terraformschema package intentionally exposes only attribute coverage.
type schemaDocument struct {
	ProviderSchemas map[string]struct {
		ResourceSchemas map[string]struct {
			Block schemaBlock `json:"block"`
		} `json:"resource_schemas"`
	} `json:"provider_schemas"`
}

type schemaBlock struct {
	Attributes map[string]json.RawMessage `json:"attributes"`
	BlockTypes map[string]struct {
		Block schemaBlock `json:"block"`
	} `json:"block_types"`
}

// mergeResourceBlock folds one resource's attributes and block_types into the
// aggregate lookup. Block names live alongside attribute names because
// Terraform state JSON serializes both under the same per-resource
// `attributes` object.
func mergeResourceBlock(dst map[string]map[string]struct{}, resourceType string, block schemaBlock) {
	bucket, ok := dst[resourceType]
	if !ok {
		bucket = make(map[string]struct{}, len(block.Attributes)+len(block.BlockTypes))
		dst[resourceType] = bucket
	}
	for attrName := range block.Attributes {
		bucket[attrName] = struct{}{}
	}
	for blockName := range block.BlockTypes {
		bucket[blockName] = struct{}{}
	}
}
