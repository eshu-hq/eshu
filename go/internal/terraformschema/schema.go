package terraformschema

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var (
	nestedIdentityBlocks = []string{"metadata"}
	identityKeyPatterns  = []string{
		"name",
		"function_name",
		"bucket",
		"family",
		"queue_name",
		"topic_name",
		"cluster_identifier",
		"cluster_id",
		"cluster_name",
		"replication_group_id",
		"domain_name",
		"broker_name",
		"table_name",
		"repository_name",
		"project_name",
		"pipeline_name",
		"app_name",
		"service_name",
		"rule_name",
		"db_name",
		"creation_token",
		"instance_id",
	}
)

type AttributeSchema struct {
	Type any `json:"type"`
}

type blockTypeSchema struct {
	Block schemaBlock `json:"block"`
}

type schemaBlock struct {
	Attributes map[string]AttributeSchema `json:"attributes"`
	BlockTypes map[string]blockTypeSchema `json:"block_types"`
}

type resourceSchema struct {
	Block schemaBlock `json:"block"`
}

type providerSchema struct {
	ResourceSchemas map[string]resourceSchema `json:"resource_schemas"`
}

type schemaDocument struct {
	FormatVersion   string                    `json:"format_version"`
	ProviderSchemas map[string]providerSchema `json:"provider_schemas"`
}

type ProviderSchemaInfo struct {
	ProviderKey           string
	ProviderName          string
	ProviderKeys          []string
	FormatVersion         string
	ResourceTypes         map[string]map[string]AttributeSchema
	ProviderResourceTypes map[string]map[string]map[string]AttributeSchema
}

func (p *ProviderSchemaInfo) ResourceCount() int {
	if p == nil {
		return 0
	}
	return len(p.ResourceTypes)
}

func LoadProviderSchema(schemaPath string) (*ProviderSchemaInfo, error) {
	if _, err := os.Stat(schemaPath); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat schema %q: %w", schemaPath, err)
	}

	raw, err := readSchemaDocument(schemaPath)
	if err != nil {
		return nil, nil
	}
	if len(raw.ProviderSchemas) == 0 {
		return nil, nil
	}

	providerKeys := make([]string, 0, len(raw.ProviderSchemas))
	for providerKey := range raw.ProviderSchemas {
		providerKeys = append(providerKeys, providerKey)
	}
	sort.Strings(providerKeys)
	providerResourceTypes := make(map[string]map[string]map[string]AttributeSchema, len(providerKeys))
	resourceTypes := make(map[string]map[string]AttributeSchema)
	for _, providerKey := range providerKeys {
		providerData := raw.ProviderSchemas[providerKey]
		providerResources := make(map[string]map[string]AttributeSchema, len(providerData.ResourceSchemas))
		for resourceType, schema := range providerData.ResourceSchemas {
			attrs := copyAttributeMap(schema.Block.Attributes)
			for _, nestedKey := range nestedIdentityBlocks {
				nested, ok := schema.Block.BlockTypes[nestedKey]
				if !ok {
					continue
				}
				for attrName, attrDef := range nested.Block.Attributes {
					if _, exists := attrs[attrName]; !exists {
						attrs[attrName] = attrDef
					}
				}
			}
			providerResources[resourceType] = attrs
			resourceTypes[resourceType] = copyAttributeMap(attrs)
		}
		providerResourceTypes[providerKey] = providerResources
	}

	return &ProviderSchemaInfo{
		ProviderKey:           providerKeys[0],
		ProviderName:          filepath.Base(providerKeys[0]),
		ProviderKeys:          providerKeys,
		FormatVersion:         valueOrDefault(raw.FormatVersion, "unknown"),
		ResourceTypes:         resourceTypes,
		ProviderResourceTypes: providerResourceTypes,
	}, nil
}

func InferIdentityKeys(attributes map[string]AttributeSchema) []string {
	for _, pattern := range identityKeyPatterns {
		attr, ok := attributes[pattern]
		if ok && isStringAttribute(attr) {
			return []string{pattern}
		}
	}

	fallback := make([]string, 0)
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, attrName := range keys {
		if !isStringAttribute(attributes[attrName]) {
			continue
		}
		if strings.HasSuffix(attrName, "_name") || strings.HasSuffix(attrName, "_identifier") {
			fallback = append(fallback, attrName)
		}
	}
	return fallback
}

// ClassifyResourceCategory returns a broad infrastructure category for a
// Terraform provider resource or data-source type.
func ClassifyResourceCategory(resourceType string) string {
	servicePart := terraformResourceServicePart(resourceType)
	if servicePart == "" {
		return "infrastructure"
	}

	if service := ClassifyResourceService(resourceType); service != "" {
		if category, ok := serviceCategories[service]; ok {
			return category
		}
	}
	return "infrastructure"
}

// ClassifyResourceService returns the provider service family for a Terraform
// resource type, using the longest known category prefix when one exists.
func ClassifyResourceService(resourceType string) string {
	servicePart := terraformResourceServicePart(resourceType)
	if servicePart == "" {
		return ""
	}
	tokens := strings.Split(servicePart, "_")
	for length := len(tokens); length > 0; length-- {
		prefix := strings.Join(tokens[:length], "_")
		if _, ok := serviceCategories[prefix]; ok {
			return prefix
		}
	}
	return tokens[0]
}

// terraformResourceServicePart strips the provider prefix from a Terraform type
// such as aws_rds_cluster so service-prefix matching can stay provider-neutral.
func terraformResourceServicePart(resourceType string) string {
	_, servicePart, ok := strings.Cut(strings.TrimSpace(resourceType), "_")
	if !ok || servicePart == "" {
		return ""
	}
	return servicePart
}

func readSchemaDocument(schemaPath string) (*schemaDocument, error) {
	var (
		file *os.File
		err  error
	)
	file, err = os.Open(schemaPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	reader := file
	if strings.HasSuffix(schemaPath, ".gz") {
		gzipReader, gzipErr := gzip.NewReader(file)
		if gzipErr != nil {
			return nil, gzipErr
		}
		defer func() {
			_ = gzipReader.Close()
		}()
		decoder := json.NewDecoder(gzipReader)
		var doc schemaDocument
		if err := decoder.Decode(&doc); err != nil {
			return nil, err
		}
		return &doc, nil
	}

	decoder := json.NewDecoder(reader)
	var doc schemaDocument
	if err := decoder.Decode(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func copyAttributeMap(attrs map[string]AttributeSchema) map[string]AttributeSchema {
	if len(attrs) == 0 {
		return map[string]AttributeSchema{}
	}
	cloned := make(map[string]AttributeSchema, len(attrs))
	for key, value := range attrs {
		cloned[key] = value
	}
	return cloned
}

func isStringAttribute(attr AttributeSchema) bool {
	typeName, ok := attr.Type.(string)
	return ok && typeName == "string"
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
