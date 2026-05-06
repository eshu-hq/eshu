// Package terraformschema loads packaged Terraform provider schemas and
// classifies resource types into ESHU-facing service and category labels.
//
// LoadProviderSchema reads gzipped or plain JSON produced by
// `terraform providers schema -json`, merges metadata-nested attributes,
// and returns a normalized ProviderSchemaInfo. InferIdentityKeys walks
// known identity attribute patterns to pick stable name keys per resource.
// ClassifyResourceCategory and ClassifyResourceService map raw resource
// types onto the curated category and service tables in categories.go.
// DefaultSchemaDir resolves the packaged schemas directory from the source
// file location and lets ESHU_TERRAFORM_SCHEMA_DIR override that path for
// focused schema tests.
package terraformschema
