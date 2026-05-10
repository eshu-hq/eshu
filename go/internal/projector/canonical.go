// Package projector canonical.go defines the canonical graph materialization
// types used to project labeled Neo4j nodes from fact envelopes. These types
// replace SourceLocalRecord as the projector's Neo4j write output.
package projector

// CanonicalMaterialization holds all canonical node and edge writes for one
// repository projection. Built from the same facts that produce content store
// writes. Written to Neo4j in strict phase order by CanonicalNodeWriter.
type CanonicalMaterialization struct {
	ScopeID                 string
	GenerationID            string
	RepoID                  string
	RepoPath                string // repository path used as Directory chain root
	FirstGeneration         bool   // true when the scope has no prior active generation
	Repository              *RepositoryRow
	Directories             []DirectoryRow
	Files                   []FileRow
	Entities                []EntityRow
	Modules                 []ModuleRow
	Imports                 []ImportRow
	Parameters              []ParameterRow
	ClassMembers            []ClassMemberRow
	NestedFuncs             []NestedFunctionRow
	TerraformStateResources []TerraformStateResourceRow
	TerraformStateModules   []TerraformStateModuleRow
	TerraformStateOutputs   []TerraformStateOutputRow
}

// IsEmpty reports whether the materialization carries no projectable data.
func (m CanonicalMaterialization) IsEmpty() bool {
	return m.Repository == nil &&
		len(m.Directories) == 0 &&
		len(m.Files) == 0 &&
		len(m.Entities) == 0 &&
		len(m.TerraformStateResources) == 0 &&
		len(m.TerraformStateModules) == 0 &&
		len(m.TerraformStateOutputs) == 0
}

// RepositoryRow carries the canonical properties for a Repository node.
type RepositoryRow struct {
	RepoID    string
	Name      string
	Path      string
	LocalPath string
	RemoteURL string
	RepoSlug  string
	HasRemote bool
}

// DirectoryRow carries the canonical properties for a Directory node.
// Directories are ordered root-first by Depth so parent nodes exist before
// children during ordered writes.
type DirectoryRow struct {
	Path       string
	Name       string
	ParentPath string // Repository.path (depth 0) or parent Directory.path
	RepoID     string
	Depth      int // 0 = first level under repo
}

// FileRow carries the canonical properties for a File node.
type FileRow struct {
	Path         string
	RelativePath string
	Name         string
	Language     string
	RepoID       string
	DirPath      string // parent Directory path for CONTAINS edge
}

// EntityRow carries the canonical properties for a labeled entity node.
// The Label field determines the Neo4j node label (Function, Class, etc.).
type EntityRow struct {
	EntityID     string
	Label        string // Neo4j label: "Function", "Class", etc.
	EntityName   string
	FilePath     string
	RelativePath string
	StartLine    int
	EndLine      int
	Language     string
	RepoID       string
	Metadata     map[string]any
}

// ModuleRow carries the canonical properties for a Module node.
type ModuleRow struct {
	Name     string
	Language string
}

// ImportRow captures one File -> Module IMPORTS edge.
type ImportRow struct {
	FilePath     string
	ModuleName   string
	ImportedName string
	Alias        string
	LineNumber   int
}

// ParameterRow captures one Function -> Parameter HAS_PARAMETER edge.
type ParameterRow struct {
	ParamName    string
	FilePath     string
	FunctionName string
	FunctionLine int
}

// ClassMemberRow captures one Class -> Function CONTAINS edge.
type ClassMemberRow struct {
	ClassName    string
	FunctionName string
	FilePath     string
	FunctionLine int
}

// NestedFunctionRow captures one Function -> Function CONTAINS edge
// for nested/inner function declarations.
type NestedFunctionRow struct {
	OuterName string
	InnerName string
	FilePath  string
	InnerLine int
}

// entityTypeLabelMap maps content store entity_type strings to their canonical
// Neo4j node labels. Every label listed here must have corresponding schema
// support in graph/schema.go, either a constraint or an intentional index.
var entityTypeLabelMap = map[string]string{
	// Code entities
	"function":                "Function",
	"class":                   "Class",
	"interface":               "Interface",
	"variable":                "Variable",
	"trait":                   "Trait",
	"struct":                  "Struct",
	"enum":                    "Enum",
	"macro":                   "Macro",
	"union":                   "Union",
	"record":                  "Record",
	"property":                "Property",
	"module":                  "Module",
	"parameter":               "Parameter",
	"annotation":              "Annotation",
	"typedef":                 "Typedef",
	"type_alias":              "TypeAlias",
	"component":               "Component",
	"impl_block":              "ImplBlock",
	"protocol":                "Protocol",
	"protocol_implementation": "ProtocolImplementation",

	// Infrastructure entities
	"k8s_resource":           "K8sResource",
	"argocd_application":     "ArgoCDApplication",
	"argocd_application_set": "ArgoCDApplicationSet",
	"crossplane_xrd":         "CrossplaneXRD",
	"crossplane_composition": "CrossplaneComposition",
	"crossplane_claim":       "CrossplaneClaim",
	"kustomize_overlay":      "KustomizeOverlay",
	"helm_chart":             "HelmChart",
	"helm_values":            "HelmValues",

	// Terraform entities
	"terraform_resource":   "TerraformResource",
	"terraform_module":     "TerraformModule",
	"terraform_variable":   "TerraformVariable",
	"terraform_output":     "TerraformOutput",
	"terraform_datasource": "TerraformDataSource",
	"terraform_provider":   "TerraformProvider",
	"terraform_local":      "TerraformLocal",
	"terragrunt_config":    "TerragruntConfig",

	// CloudFormation entities
	"cloudformation_resource":  "CloudFormationResource",
	"cloudformation_parameter": "CloudFormationParameter",
	"cloudformation_output":    "CloudFormationOutput",

	// SQL entities
	"sql_table":    "SqlTable",
	"sql_view":     "SqlView",
	"sql_function": "SqlFunction",
	"sql_trigger":  "SqlTrigger",
	"sql_index":    "SqlIndex",
	"sql_column":   "SqlColumn",

	// Data entities
	"data_asset":         "DataAsset",
	"data_column":        "DataColumn",
	"analytics_model":    "AnalyticsModel",
	"dashboard_asset":    "DashboardAsset",
	"data_quality_check": "DataQualityCheck",
	"query_execution":    "QueryExecution",
	"data_contract":      "DataContract",
	"data_owner":         "DataOwner",

	// Terragrunt extended types (emitted by parser as PascalCase, added as
	// lowercase aliases for completeness).
	"terragrunt_dependency": "TerragruntDependency",
	"terragrunt_input":      "TerragruntInput",
	"terragrunt_local":      "TerragruntLocal",

	// Type annotation entities
	"type_annotation": "TypeAnnotation",
}

// entityTypeLabelValues is the reverse set of entityTypeLabelMap — every
// distinct Neo4j label that appears as a value. Initialised at package init
// so that EntityTypeLabel can recognise PascalCase inputs from the parser.
var entityTypeLabelValues map[string]struct{}

func init() {
	entityTypeLabelValues = make(map[string]struct{}, len(entityTypeLabelMap))
	for _, label := range entityTypeLabelMap {
		entityTypeLabelValues[label] = struct{}{}
	}
}

// EntityTypeLabel returns the Neo4j label for a content store entity type.
// Handles both lowercase keys ("function") used in the map definition and
// PascalCase values ("Function") emitted by the Go parser. Returns the
// label and true if found, empty string and false otherwise.
func EntityTypeLabel(entityType string) (string, bool) {
	// Try exact match first (lowercase keys).
	if label, ok := entityTypeLabelMap[entityType]; ok {
		return label, true
	}

	// The Go parser emits PascalCase entity types that match Neo4j labels
	// directly (e.g. "Function", "K8sResource"). Check if the input is
	// itself a valid label value.
	if _, isLabel := entityTypeLabelValues[entityType]; isLabel {
		return entityType, true
	}

	return "", false
}

// EntityTypeLabelMap returns a copy of the entity type to Neo4j label mapping.
// Useful for testing completeness against schema constraints.
func EntityTypeLabelMap() map[string]string {
	cloned := make(map[string]string, len(entityTypeLabelMap))
	for k, v := range entityTypeLabelMap {
		cloned[k] = v
	}
	return cloned
}
