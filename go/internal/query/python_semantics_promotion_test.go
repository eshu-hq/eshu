package query

import (
	"reflect"
	"testing"
)

func TestPythonSemanticProfileFromMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		entityType  string
		metadata    map[string]any
		wantSignals []PythonSemanticSignal
		wantKind    string
	}{
		{
			name:       "decorated async function with annotations",
			entityType: "Function",
			metadata: map[string]any{
				"decorators":       []any{"@route", "@cached"},
				"async":            true,
				"type_annotations": []any{map[string]any{"annotation_kind": "parameter"}},
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalDecorator,
				PythonSemanticSignalAsync,
				PythonSemanticSignalTypeAnnotation,
			},
			wantKind: "decorated_async_function",
		},
		{
			name:       "function with compact type annotation projection",
			entityType: "Function",
			metadata: map[string]any{
				"type_annotation_count": 2,
				"type_annotation_kinds": []any{"parameter", "return"},
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalTypeAnnotation,
			},
			wantKind: "type_annotation",
		},
		{
			name:       "type annotation entity",
			entityType: "TypeAnnotation",
			metadata: map[string]any{
				"type":            "str",
				"annotation_kind": "parameter",
				"context":         "greet",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalTypeAnnotation,
			},
			wantKind: "parameter_type_annotation",
		},
		{
			name:       "return type annotation entity",
			entityType: "TypeAnnotation",
			metadata: map[string]any{
				"type":            "str",
				"annotation_kind": "return",
				"context":         "greet",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalTypeAnnotation,
			},
			wantKind: "return_type_annotation",
		},
		{
			name:       "lambda function",
			entityType: "Function",
			metadata: map[string]any{
				"semantic_kind": "lambda",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalLambda,
			},
			wantKind: "lambda_function",
		},
		{
			name:       "generator function",
			entityType: "Function",
			metadata: map[string]any{
				"semantic_kind": "generator",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalGenerator,
			},
			wantKind: "generator_function",
		},
		{
			name:       "class with metaclass",
			entityType: "Class",
			metadata: map[string]any{
				"metaclass": "MetaLogger",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalMetaclass,
			},
			wantKind: "metaclass_class",
		},
		{
			name:       "class with docstring only",
			entityType: "Class",
			metadata: map[string]any{
				"docstring": "Represents a configured logger.",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalDocstring,
			},
			wantKind: "documented_class",
		},
		{
			name:       "module with docstring only",
			entityType: "Module",
			metadata: map[string]any{
				"docstring": "Utilities for payments.",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalDocstring,
			},
			wantKind: "documented_module",
		},
		{
			name:       "function with docstring only",
			entityType: "Function",
			metadata: map[string]any{
				"docstring": "Handles incoming requests.",
			},
			wantSignals: []PythonSemanticSignal{
				PythonSemanticSignalDocstring,
			},
			wantKind: "documented_function",
		},
		{
			name:        "plain python function",
			entityType:  "Function",
			metadata:    map[string]any{},
			wantSignals: nil,
			wantKind:    "plain",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			profile := PythonSemanticProfileFromMetadata(tt.entityType, tt.metadata)

			if got, want := profile.Signals(), tt.wantSignals; !reflect.DeepEqual(got, want) {
				t.Fatalf("Signals() = %#v, want %#v", got, want)
			}
			if got, want := profile.SurfaceKind(), tt.wantKind; got != want {
				t.Fatalf("SurfaceKind() = %q, want %q", got, want)
			}
		})
	}
}

func TestPythonSemanticProfilePriority(t *testing.T) {
	t.Parallel()

	profile := PythonSemanticProfileFromMetadata("Function", map[string]any{
		"decorators": []any{"@route"},
		"async":      true,
	})

	if !profile.HasSignals() {
		t.Fatal("HasSignals() = false, want true")
	}
	if got, want := profile.PrimarySignal(), PythonSemanticSignalDecorator; got != want {
		t.Fatalf("PrimarySignal() = %q, want %q", got, want)
	}
}

func TestAttachPythonSemanticsClonesResult(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"entity_id": "graph-1",
		"name":      "handler",
	}

	got := AttachPythonSemantics(result, map[string]any{
		"decorators": []any{"@route"},
		"async":      true,
		"docstring":  "Handles incoming requests.",
	})

	if _, ok := result["python_semantics"]; ok {
		t.Fatal("result was mutated, want original map unchanged")
	}

	semantics, ok := got["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("python_semantics type = %T, want map[string]any", got["python_semantics"])
	}
	if got, want := semantics["decorators"], []string{"@route"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("python_semantics[decorators] = %#v, want %#v", got, want)
	}
	if got, want := semantics["async"], true; got != want {
		t.Fatalf("python_semantics[async] = %#v, want %#v", got, want)
	}
	if got, want := semantics["docstring"], "Handles incoming requests."; got != want {
		t.Fatalf("python_semantics[docstring] = %#v, want %#v", got, want)
	}
	if got, want := semantics["surface_kind"], "decorated_async_function"; got != want {
		t.Fatalf("python_semantics[surface_kind] = %#v, want %#v", got, want)
	}
	signals, ok := semantics["signals"].([]string)
	if !ok {
		t.Fatalf("python_semantics[signals] type = %T, want []string", semantics["signals"])
	}
	if !reflect.DeepEqual(signals, []string{"decorator", "async", "docstring"}) {
		t.Fatalf("python_semantics[signals] = %#v, want [decorator async docstring]", signals)
	}
}

func TestPythonSemanticProfileFromMetadataDocstringSignal(t *testing.T) {
	t.Parallel()

	profile := PythonSemanticProfileFromMetadata("Module", map[string]any{
		"docstring": "Utilities for payments.",
	})

	if !profile.HasSignals() {
		t.Fatal("HasSignals() = false, want true")
	}
	if got, want := profile.PrimarySignal(), PythonSemanticSignalDocstring; got != want {
		t.Fatalf("PrimarySignal() = %q, want %q", got, want)
	}
	if got, want := profile.Signals(), []PythonSemanticSignal{PythonSemanticSignalDocstring}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Signals() = %#v, want %#v", got, want)
	}
}

func TestAttachPythonSemanticsReturnsOriginalWhenEmpty(t *testing.T) {
	t.Parallel()

	result := map[string]any{
		"entity_id": "graph-1",
	}

	got := AttachPythonSemantics(result, map[string]any{})

	if _, ok := got["python_semantics"]; ok {
		t.Fatal("python_semantics present, want absent")
	}
	if got["entity_id"] != "graph-1" {
		t.Fatalf("entity_id = %#v, want graph-1", got["entity_id"])
	}
}
