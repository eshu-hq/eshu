package shape

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/content"
)

func TestMaterializeBuildsFileRecordsAndOrderedEntities(t *testing.T) {
	t.Parallel()

	input := Input{
		RepoID:       "repository:r_12345678",
		SourceSystem: "git",
		Files: []File{
			{
				Path:            "src/app.py",
				Body:            "def alpha():\n  return 1\n\ndef beta():\n  return 2\n\nclass Widget:\n  pass\n\nterraform {\n  required_providers {\n    aws = {\n      source = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}\n",
				Digest:          "digest-1",
				Language:        "python",
				ArtifactType:    "source",
				TemplateDialect: "jinja",
				IACRelevant:     boolPtr(true),
				CommitSHA:       "abc123",
				Metadata: map[string]string{
					"custom": "value",
				},
				EntityBuckets: map[string][]Entity{
					"classes": {
						{
							Name:       "Widget",
							LineNumber: 7,
						},
					},
					"functions": {
						{
							Name:       "alpha",
							LineNumber: 1,
							Source:     "def alpha():\n  return 1",
							Metadata: map[string]any{
								"docstring":   "Alpha docs.",
								"decorators":  []string{"@cached"},
								"async":       true,
								"nested_data": map[string]any{"team": "platform"},
							},
						},
						{
							Name:       "beta",
							LineNumber: 4,
						},
					},
					"terraform_blocks": {
						{
							Name:       "terraform",
							LineNumber: 10,
							Metadata: map[string]any{
								"required_providers":        "aws",
								"required_provider_sources": "aws=hashicorp/aws",
								"required_provider_count":   1,
							},
						},
					},
					"ignored_bucket": {
						{
							Name:       "Ignored",
							LineNumber: 9,
						},
					},
				},
			},
		},
	}

	got, err := Materialize(input)
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got.RepoID != input.RepoID {
		t.Fatalf("Materialize().RepoID = %q, want %q", got.RepoID, input.RepoID)
	}
	if got.SourceSystem != input.SourceSystem {
		t.Fatalf("Materialize().SourceSystem = %q, want %q", got.SourceSystem, input.SourceSystem)
	}
	if len(got.Records) != 1 {
		t.Fatalf("len(Materialize().Records) = %d, want 1", len(got.Records))
	}

	record := got.Records[0]
	if record.Path != "src/app.py" {
		t.Fatalf("record.Path = %q, want %q", record.Path, "src/app.py")
	}
	if record.Body != input.Files[0].Body {
		t.Fatalf("record.Body = %q, want %q", record.Body, input.Files[0].Body)
	}
	if record.Digest != "digest-1" {
		t.Fatalf("record.Digest = %q, want %q", record.Digest, "digest-1")
	}
	if record.Metadata["custom"] != "value" {
		t.Fatalf("record.Metadata[custom] = %q, want %q", record.Metadata["custom"], "value")
	}
	if record.Metadata["language"] != "python" {
		t.Fatalf("record.Metadata[language] = %q, want %q", record.Metadata["language"], "python")
	}
	if record.Metadata["artifact_type"] != "source" {
		t.Fatalf("record.Metadata[artifact_type] = %q, want %q", record.Metadata["artifact_type"], "source")
	}
	if record.Metadata["template_dialect"] != "jinja" {
		t.Fatalf("record.Metadata[template_dialect] = %q, want %q", record.Metadata["template_dialect"], "jinja")
	}
	if record.Metadata["iac_relevant"] != "true" {
		t.Fatalf("record.Metadata[iac_relevant] = %q, want %q", record.Metadata["iac_relevant"], "true")
	}
	if record.Metadata["commit_sha"] != "abc123" {
		t.Fatalf("record.Metadata[commit_sha] = %q, want %q", record.Metadata["commit_sha"], "abc123")
	}

	if len(got.Entities) != 4 {
		t.Fatalf("len(Materialize().Entities) = %d, want 4", len(got.Entities))
	}

	wantEntities := []EntityRecordExpectation{
		{
			entityType:  "Function",
			entityName:  "alpha",
			startLine:   1,
			endLine:     3,
			sourceCache: "def alpha():\n  return 1\n",
			entityID:    content.CanonicalEntityID("repository:r_12345678", "src/app.py", "Function", "alpha", 1),
		},
		{
			entityType:  "Function",
			entityName:  "beta",
			startLine:   4,
			endLine:     6,
			sourceCache: "def beta():\n  return 2\n",
			entityID:    content.CanonicalEntityID("repository:r_12345678", "src/app.py", "Function", "beta", 4),
		},
		{
			entityType:  "Class",
			entityName:  "Widget",
			startLine:   7,
			endLine:     9,
			sourceCache: "class Widget:\n  pass\n",
			entityID:    content.CanonicalEntityID("repository:r_12345678", "src/app.py", "Class", "Widget", 7),
		},
		{
			entityType:  "TerraformBlock",
			entityName:  "terraform",
			startLine:   10,
			endLine:     17,
			sourceCache: "terraform {\n  required_providers {\n    aws = {\n      source = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}",
			entityID:    content.CanonicalEntityID("repository:r_12345678", "src/app.py", "TerraformBlock", "terraform", 10),
		},
	}

	for i, want := range wantEntities {
		gotEntity := got.Entities[i]
		if gotEntity.EntityType != want.entityType {
			t.Fatalf("entity[%d].EntityType = %q, want %q", i, gotEntity.EntityType, want.entityType)
		}
		if gotEntity.EntityName != want.entityName {
			t.Fatalf("entity[%d].EntityName = %q, want %q", i, gotEntity.EntityName, want.entityName)
		}
		if gotEntity.StartLine != want.startLine {
			t.Fatalf("entity[%d].StartLine = %d, want %d", i, gotEntity.StartLine, want.startLine)
		}
		if gotEntity.EndLine != want.endLine {
			t.Fatalf("entity[%d].EndLine = %d, want %d", i, gotEntity.EndLine, want.endLine)
		}
		if want.entityType != "TerraformBlock" && gotEntity.SourceCache != want.sourceCache {
			t.Fatalf("entity[%d].SourceCache = %q, want %q", i, gotEntity.SourceCache, want.sourceCache)
		}
		if gotEntity.EntityID != want.entityID {
			t.Fatalf("entity[%d].EntityID = %q, want %q", i, gotEntity.EntityID, want.entityID)
		}
		if gotEntity.Path != "src/app.py" {
			t.Fatalf("entity[%d].Path = %q, want %q", i, gotEntity.Path, "src/app.py")
		}
	}
	if got, want := got.Entities[0].Metadata["docstring"], "Alpha docs."; got != want {
		t.Fatalf("entity[0].Metadata[docstring] = %#v, want %#v", got, want)
	}
	if got, want := got.Entities[0].Metadata["async"], true; got != want {
		t.Fatalf("entity[0].Metadata[async] = %#v, want %#v", got, want)
	}
	if got, want := got.Entities[0].Metadata["decorators"], []string{"@cached"}; !stringSlicesEqual(toStringSlice(got), want) {
		t.Fatalf("entity[0].Metadata[decorators] = %#v, want %#v", got, want)
	}
	nested, ok := got.Entities[0].Metadata["nested_data"].(map[string]any)
	if !ok {
		t.Fatalf("entity[0].Metadata[nested_data] = %T, want map[string]any", got.Entities[0].Metadata["nested_data"])
	}
	if got, want := nested["team"], "platform"; got != want {
		t.Fatalf("entity[0].Metadata[nested_data][team] = %#v, want %#v", got, want)
	}
	if got, want := got.Entities[3].Metadata["required_providers"], "aws"; got != want {
		t.Fatalf("entity[3].Metadata[required_providers] = %#v, want %#v", got, want)
	}
	if got, want := got.Entities[3].Metadata["required_provider_sources"], "aws=hashicorp/aws"; got != want {
		t.Fatalf("entity[3].Metadata[required_provider_sources] = %#v, want %#v", got, want)
	}
	if got, want := got.Entities[3].Metadata["required_provider_count"], 1; got != want {
		t.Fatalf("entity[3].Metadata[required_provider_count] = %#v, want %#v", got, want)
	}
	if got, want := strings.TrimPrefix(got.Entities[3].SourceCache, "\n"), "terraform {\n  required_providers {\n    aws = {\n      source = \"hashicorp/aws\"\n      version = \"~> 5.0\"\n    }\n  }\n}"; got != want {
		t.Fatalf("entity[3].SourceCache = %#v, want %#v", got, want)
	}
}

func TestMaterializeDefaultsInvalidStartLineToOne(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "schema.sql",
				Body: "",
				EntityBuckets: map[string][]Entity{
					"functions": {
						{
							Name:       "process",
							LineNumber: 0,
							Source:     "process()",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}
	entity := got.Entities[0]
	if entity.StartLine != 1 {
		t.Fatalf("entity.StartLine = %d, want 1", entity.StartLine)
	}
	if entity.EndLine != 1 {
		t.Fatalf("entity.EndLine = %d, want 1", entity.EndLine)
	}
	if entity.SourceCache != "process()\n" {
		t.Fatalf("entity.SourceCache = %q, want %q", entity.SourceCache, "process()\n")
	}
	if entity.EntityID != content.CanonicalEntityID("repository:r_12345678", "schema.sql", "Function", "process", 1) {
		t.Fatalf("entity.EntityID = %q, want canonical id", entity.EntityID)
	}
}

func TestMaterializeCarriesExtendedParserEntityBuckets(t *testing.T) {
	t.Parallel()

	input := Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "src/parity.tsx",
				Body: "content\n",
				EntityBuckets: map[string][]Entity{
					"modules": {
						{Name: "Demo.Module", LineNumber: 1},
					},
					"protocols": {
						{Name: "Runnable", LineNumber: 2},
					},
					"type_aliases": {
						{Name: "WidgetProps", LineNumber: 3},
					},
					"type_annotations": {
						{Name: "name", LineNumber: 4},
					},
					"typedefs": {
						{Name: "my_int", LineNumber: 5},
					},
					"components": {
						{Name: "ToolbarButton", LineNumber: 6},
					},
				},
			},
		},
	}

	got, err := Materialize(input)
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 6 {
		t.Fatalf("len(Materialize().Entities) = %d, want 6", len(got.Entities))
	}

	wantTypes := []string{
		"Module",
		"Protocol",
		"TypeAlias",
		"TypeAnnotation",
		"Typedef",
		"Component",
	}
	gotTypes := make([]string, 0, len(got.Entities))
	for _, entity := range got.Entities {
		gotTypes = append(gotTypes, entity.EntityType)
	}
	if len(gotTypes) != len(wantTypes) {
		t.Fatalf("entity type count = %d, want %d", len(gotTypes), len(wantTypes))
	}
	for i, want := range wantTypes {
		if gotTypes[i] != want {
			t.Fatalf("entity[%d].EntityType = %q, want %q", i, gotTypes[i], want)
		}
	}
}

func TestMaterializeCarriesRustImplBlockEntities(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "src/lib.rs",
				Body: "impl Point {\n    fn new() -> Self {\n        Self {}\n    }\n}\n",
				EntityBuckets: map[string][]Entity{
					"impl_blocks": {
						{
							Name:       "Point",
							LineNumber: 1,
							EndLine:    5,
							Metadata: map[string]any{
								"kind":   "inherent_impl",
								"target": "Point",
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if got, want := len(got.Entities), 1; got != want {
		t.Fatalf("len(Materialize().Entities) = %d, want %d", got, want)
	}
	entity := got.Entities[0]
	if got, want := entity.EntityType, "ImplBlock"; got != want {
		t.Fatalf("entity.EntityType = %q, want %q", got, want)
	}
	if got, want := entity.EntityName, "Point"; got != want {
		t.Fatalf("entity.EntityName = %q, want %q", got, want)
	}
	if got, want := entity.StartLine, 1; got != want {
		t.Fatalf("entity.StartLine = %d, want %d", got, want)
	}
	if got, want := entity.EndLine, 5; got != want {
		t.Fatalf("entity.EndLine = %d, want %d", got, want)
	}
	if got, want := entity.SourceCache, "impl Point {\n    fn new() -> Self {\n        Self {}\n    }\n}\n"; got != want {
		t.Fatalf("entity.SourceCache = %q, want %q", got, want)
	}
	if got, want := entity.Metadata["kind"], "inherent_impl"; got != want {
		t.Fatalf("entity.Metadata[kind] = %#v, want %#v", got, want)
	}
	if got, want := entity.Metadata["target"], "Point"; got != want {
		t.Fatalf("entity.Metadata[target] = %#v, want %#v", got, want)
	}
}

func TestMaterializePromotesElixirDefimplToProtocolImplementation(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "lib/demo/serializable.ex",
				Body: "defimpl Demo.Serializable, for: Demo.Worker do\n  def serialize(worker), do: worker\nend\n",
				EntityBuckets: map[string][]Entity{
					"modules": {
						{
							Name:       "Demo.Serializable",
							LineNumber: 1,
							EndLine:    3,
							Source:     "defimpl Demo.Serializable, for: Demo.Worker do\n  def serialize(worker), do: worker\nend",
							Metadata: map[string]any{
								"module_kind":     "protocol_implementation",
								"protocol":        "Demo.Serializable",
								"implemented_for": "Demo.Worker",
							},
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 1 {
		t.Fatalf("len(Materialize().Entities) = %d, want 1", len(got.Entities))
	}

	entity := got.Entities[0]
	if got, want := entity.EntityType, "ProtocolImplementation"; got != want {
		t.Fatalf("entity.EntityType = %q, want %q", got, want)
	}
	if got, want := entity.EntityName, "Demo.Serializable"; got != want {
		t.Fatalf("entity.EntityName = %q, want %q", got, want)
	}
	if got, want := entity.StartLine, 1; got != want {
		t.Fatalf("entity.StartLine = %d, want %d", got, want)
	}
	if got, want := entity.EndLine, 3; got != want {
		t.Fatalf("entity.EndLine = %d, want %d", got, want)
	}
	if got, want := entity.Metadata["module_kind"], "protocol_implementation"; got != want {
		t.Fatalf("entity.Metadata[module_kind] = %#v, want %#v", got, want)
	}
	if got, want := entity.Metadata["protocol"], "Demo.Serializable"; got != want {
		t.Fatalf("entity.Metadata[protocol] = %#v, want %#v", got, want)
	}
	if got, want := entity.Metadata["implemented_for"], "Demo.Worker"; got != want {
		t.Fatalf("entity.Metadata[implemented_for] = %#v, want %#v", got, want)
	}
}

func TestMaterializeCarriesCloudFormationExtendedBuckets(t *testing.T) {
	t.Parallel()

	got, err := Materialize(Input{
		RepoID: "repository:r_12345678",
		Files: []File{
			{
				Path: "infra/stack.yaml",
				Body: "AWSTemplateFormatVersion: \"2010-09-09\"\n",
				EntityBuckets: map[string][]Entity{
					"cloudformation_conditions": {
						{Name: "EnableNested", LineNumber: 1, Metadata: map[string]any{"expression": "map[Fn::Equals:[prod prod]]"}},
					},
					"cloudformation_cross_stack_imports": {
						{Name: "SharedVpcId", LineNumber: 2},
					},
					"cloudformation_cross_stack_exports": {
						{Name: "Stack-BucketArn", LineNumber: 3},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v, want nil", err)
	}

	if len(got.Entities) != 3 {
		t.Fatalf("len(Materialize().Entities) = %d, want 3", len(got.Entities))
	}

	wantTypes := []string{
		"CloudFormationCondition",
		"CloudFormationImport",
		"CloudFormationExport",
	}
	for index, want := range wantTypes {
		if got.Entities[index].EntityType != want {
			t.Fatalf("entity[%d].EntityType = %q, want %q", index, got.Entities[index].EntityType, want)
		}
	}
}
