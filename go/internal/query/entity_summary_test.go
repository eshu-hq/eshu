package query

import "testing"

func TestBuildEntitySemanticSummaryTypeScriptClass(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Class"},
		"name":   "Demo",
		"metadata": map[string]any{
			"decorators":      []any{"@sealed"},
			"type_parameters": []any{"T"},
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Class Demo uses decorators @sealed and declares type parameters T."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonFunction(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "handler",
		"metadata": map[string]any{
			"decorators": []any{"@route"},
			"async":      true,
			"docstring":  "Handles incoming requests.",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function handler is async, uses decorators @route, and is documented as \"Handles incoming requests.\"."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonDecoratedClass(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels":   []string{"Class"},
		"name":     "Logged",
		"language": "python",
		"metadata": map[string]any{
			"decorators": []any{"@tracked"},
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Class Logged is decorated with @tracked."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonAsyncFunction(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "run",
		"metadata": map[string]any{
			"async": true,
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function run is async."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonModuleDocstring(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Module"},
		"name":   "module_docstring",
		"metadata": map[string]any{
			"docstring": "Utilities for payments.",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Module module_docstring is documented as \"Utilities for payments.\"."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonFunctionTypeAnnotations(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "greet",
		"metadata": map[string]any{
			"type_annotation_count": 2,
			"type_annotation_kinds": []any{"parameter", "return"},
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function greet has parameter and return type annotations."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonLambdaFunction(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "double",
		"metadata": map[string]any{
			"semantic_kind": "lambda",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function double is a lambda function."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryPythonMetaclassClass(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Class"},
		"name":   "Logged",
		"metadata": map[string]any{
			"metaclass": "MetaLogger",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Class Logged uses metaclass MetaLogger."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryTypeAnnotation(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"TypeAnnotation"},
		"name":   "name",
		"metadata": map[string]any{
			"type":            "str",
			"annotation_kind": "parameter",
			"context":         "greet",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "TypeAnnotation name is a parameter annotation for greet with type str."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryTypeAnnotationReturn(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"TypeAnnotation"},
		"name":   "greet",
		"metadata": map[string]any{
			"type":            "str",
			"annotation_kind": "return",
			"context":         "greet",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "TypeAnnotation greet is a return annotation for greet with type str."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryComponent(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Component"},
		"name":   "Button",
		"metadata": map[string]any{
			"framework": "react",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Component Button is associated with the react framework."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryTypedef(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Typedef"},
		"name":   "my_int",
		"metadata": map[string]any{
			"type": "int",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Typedef my_int aliases int."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryAnnotation(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Annotation"},
		"name":   "Logged",
		"metadata": map[string]any{
			"kind":        "applied",
			"target_kind": "method_declaration",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Annotation Logged is applied to a method declaration."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryProtocol(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Protocol"},
		"name":   "Demo.Serializable",
		"metadata": map[string]any{
			"module_kind": "protocol",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Protocol Demo.Serializable is a protocol."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryRustImplBlock(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"ImplBlock"},
		"name":   "Point",
		"metadata": map[string]any{
			"kind":   "trait_impl",
			"trait":  "Display",
			"target": "Point",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "ImplBlock Point implements Display for Point."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryKotlinSecondaryConstructor(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "Widget",
		"metadata": map[string]any{
			"constructor_kind": "secondary",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function Widget is a secondary constructor."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryElixirModule(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Module"},
		"name":   "Demo.Serializable",
		"metadata": map[string]any{
			"module_kind":     "protocol_implementation",
			"protocol":        "Demo.Serializable",
			"implemented_for": "Demo.Worker",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Module Demo.Serializable is a protocol implementation for Demo.Worker via Demo.Serializable."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryElixirFunctionKinds(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "is_even",
		"metadata": map[string]any{
			"semantic_kind": "guard",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function is_even is a guard."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryElixirModuleAttribute(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Variable"},
		"name":   "@timeout",
		"metadata": map[string]any{
			"attribute_kind": "module_attribute",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Variable @timeout is a module attribute."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryJavaScriptFunction(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels":   []string{"Function"},
		"name":     "getTab",
		"language": "javascript",
		"metadata": map[string]any{
			"docstring":   "Returns the active tab.",
			"method_kind": "getter",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function getTab has JavaScript method kind getter and is documented as \"Returns the active tab.\"."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryJavaScriptGeneratorFunction(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels":   []string{"Function"},
		"name":     "createIds",
		"language": "javascript",
		"metadata": map[string]any{
			"semantic_kind": "generator",
		},
	}

	got := buildEntitySemanticSummary(entity)
	want := "Function createIds is a generator."
	if got != want {
		t.Fatalf("buildEntitySemanticSummary() = %q, want %q", got, want)
	}
}

func TestBuildEntitySemanticSummaryEmptyWhenNoUsefulMetadata(t *testing.T) {
	t.Parallel()

	entity := map[string]any{
		"labels": []string{"Function"},
		"name":   "noop",
	}

	if got := buildEntitySemanticSummary(entity); got != "" {
		t.Fatalf("buildEntitySemanticSummary() = %q, want empty", got)
	}
}
