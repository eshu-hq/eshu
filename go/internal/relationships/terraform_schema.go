package relationships

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/eshu-hq/eshu/go/internal/terraformschema"
)

const (
	terraformIdentityKeyConfidence      = 0.78
	terraformResourceNameFallbackWeight = 0.55
)

var (
	terraformResourceBlockPattern = regexp.MustCompile(`(?is)resource\s+"([^"]+)"\s+"([^"]+)"\s*\{(.*?)\n\}`)
	terraformQuotedValuePattern   = regexp.MustCompile(`(?i)\b([A-Za-z0-9_]+)\b\s*=\s*"([^"]+)"`)
	genericTerraformResourceNames = map[string]struct{}{
		"main":    {},
		"this":    {},
		"default": {},
		"primary": {},
		"example": {},
		"test":    {},
		"temp":    {},
		"tmp":     {},
	}

	terraformSchemaRegistryMu   sync.RWMutex
	terraformSchemaBootstrap    bool
	terraformResourceExtractors = map[string][]terraformResourceExtractor{}
)

type terraformResourceRelationship struct {
	EvidenceKind     EvidenceKind
	RelationshipType RelationshipType
	Confidence       float64
	Rationale        string
	CandidateName    string
	Details          map[string]any
}

type terraformResourceExtractor func(resourceType, resourceName, body string) []terraformResourceRelationship

func RegisterSchemaDrivenTerraformExtractors(schemaDir string) map[string]int {
	if schemaDir == "" {
		return map[string]int{}
	}
	info, err := os.Stat(schemaDir)
	if err != nil || !info.IsDir() {
		return map[string]int{}
	}

	terraformSchemaRegistryMu.Lock()
	defer terraformSchemaRegistryMu.Unlock()

	summary := map[string]int{}
	matches, err := filepath.Glob(filepath.Join(schemaDir, "*.json*"))
	if err != nil {
		return summary
	}
	sort.Strings(matches)

	for _, schemaPath := range matches {
		schema, err := terraformschema.LoadProviderSchema(schemaPath)
		if err != nil || schema == nil {
			continue
		}

		providerResourceTypes := schema.ProviderResourceTypes
		if len(providerResourceTypes) == 0 && len(schema.ResourceTypes) > 0 {
			providerResourceTypes = map[string]map[string]map[string]terraformschema.AttributeSchema{
				schema.ProviderKey: schema.ResourceTypes,
			}
		}

		providerKeys := schema.ProviderKeys
		if len(providerKeys) == 0 {
			providerKeys = make([]string, 0, len(providerResourceTypes))
			for providerKey := range providerResourceTypes {
				providerKeys = append(providerKeys, providerKey)
			}
			sort.Strings(providerKeys)
		}

		for _, providerKey := range providerKeys {
			resourceTypes := providerResourceTypes[providerKey]
			if len(resourceTypes) == 0 {
				continue
			}

			registeredCount := 0
			for resourceType, attributes := range resourceTypes {
				normalized := normalizeResourceType(resourceType)
				if _, exists := terraformResourceExtractors[normalized]; exists {
					continue
				}

				identityKeys := terraformschema.InferIdentityKeys(attributes)
				category := terraformschema.ClassifyResourceCategory(resourceType)
				terraformResourceExtractors[normalized] = []terraformResourceExtractor{
					makeTerraformSchemaExtractor(identityKeys, category),
				}
				registeredCount++
			}
			summary[filepath.Base(providerKey)] += registeredCount
		}
	}

	if len(summary) > 0 {
		terraformSchemaBootstrap = true
	}
	return summary
}

func getTerraformResourceExtractors(resourceType string) []terraformResourceExtractor {
	terraformSchemaRegistryMu.RLock()
	defer terraformSchemaRegistryMu.RUnlock()

	registered := terraformResourceExtractors[normalizeResourceType(resourceType)]
	if len(registered) == 0 {
		return nil
	}
	cloned := make([]terraformResourceExtractor, len(registered))
	copy(cloned, registered)
	return cloned
}

func ensureDefaultTerraformSchemaExtractors() {
	terraformSchemaRegistryMu.RLock()
	bootstrapped := terraformSchemaBootstrap
	terraformSchemaRegistryMu.RUnlock()
	if bootstrapped {
		return
	}

	RegisterSchemaDrivenTerraformExtractors(defaultTerraformSchemaDir())
}

func discoverTerraformSchemaEvidence(
	sourceRepoID, filePath, content string,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	ensureDefaultTerraformSchemaExtractors()

	var evidence []EvidenceFact
	for _, match := range terraformResourceBlockPattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 4 {
			continue
		}
		resourceType := normalizeResourceType(match[1])
		resourceName := strings.TrimSpace(match[2])
		body := match[3]

		for _, extractor := range getTerraformResourceExtractors(resourceType) {
			for _, rel := range extractor(resourceType, resourceName, body) {
				evidence = append(evidence, matchCatalog(
					sourceRepoID,
					rel.CandidateName,
					filePath,
					rel.EvidenceKind,
					rel.RelationshipType,
					rel.Confidence,
					rel.Rationale,
					"terraform-schema",
					catalog,
					seen,
					rel.Details,
				)...)
			}
		}
	}
	return evidence
}

func makeTerraformSchemaExtractor(
	identityKeys []string,
	category string,
) terraformResourceExtractor {
	return func(resourceType, resourceName, body string) []terraformResourceRelationship {
		candidate := ""
		matchedKey := ""
		confidence := terraformIdentityKeyConfidence

		for _, key := range identityKeys {
			value := firstTerraformQuotedValue(body, key)
			if value == "" {
				continue
			}
			candidate = value
			matchedKey = key
			break
		}

		if candidate == "" {
			lowerName := strings.ToLower(strings.TrimSpace(resourceName))
			if _, generic := genericTerraformResourceNames[lowerName]; generic || lowerName == "" {
				return nil
			}
			candidate = resourceName
			matchedKey = "resource_name"
			confidence = terraformResourceNameFallbackWeight
		}

		_, suffix, ok := strings.Cut(resourceType, "_")
		if !ok || suffix == "" {
			suffix = resourceType
		}

		return []terraformResourceRelationship{
			{
				EvidenceKind:     EvidenceKind("TERRAFORM_" + strings.ToUpper(suffix)),
				RelationshipType: RelProvisionsDependencyFor,
				Confidence:       confidence,
				Rationale:        "Terraform " + resourceType + " provisions " + category + " infrastructure",
				CandidateName:    candidate,
				Details: map[string]any{
					"resource_type": resourceType,
					"resource_name": resourceName,
					"identity_key":  matchedKey,
					"category":      category,
					"schema_driven": true,
				},
			},
		}
	}
}

func defaultTerraformSchemaDir() string {
	return terraformschema.DefaultSchemaDir()
}

func normalizeResourceType(resourceType string) string {
	return strings.ToLower(strings.TrimSpace(resourceType))
}

func firstTerraformQuotedValue(content, key string) string {
	for _, match := range terraformQuotedValuePattern.FindAllStringSubmatch(content, -1) {
		if len(match) < 3 {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(match[1]), key) {
			continue
		}
		value := strings.TrimSpace(match[2])
		if value != "" {
			return value
		}
	}
	return ""
}
