package query

import (
	"maps"
	"strings"
)

// PythonSemanticSignal identifies a Python-specific semantic that should be
// promoted in query surfaces.
type PythonSemanticSignal string

const (
	// PythonSemanticSignalDecorator marks decorator-driven behavior.
	PythonSemanticSignalDecorator PythonSemanticSignal = "decorator"
	// PythonSemanticSignalAsync marks async function behavior.
	PythonSemanticSignalAsync PythonSemanticSignal = "async"
	// PythonSemanticSignalGenerator marks generator function behavior.
	PythonSemanticSignalGenerator PythonSemanticSignal = "generator"
	// PythonSemanticSignalLambda marks lambda-assignment behavior.
	PythonSemanticSignalLambda PythonSemanticSignal = "lambda"
	// PythonSemanticSignalMetaclass marks class metaclass ownership.
	PythonSemanticSignalMetaclass PythonSemanticSignal = "metaclass"
	// PythonSemanticSignalDocstring marks docstring-only behavior.
	PythonSemanticSignalDocstring PythonSemanticSignal = "docstring"
	// PythonSemanticSignalTypeAnnotation marks type annotation behavior.
	PythonSemanticSignalTypeAnnotation PythonSemanticSignal = "type_annotation"
)

// PythonSemanticProfile summarizes the highest-signal Python metadata a query
// result carries so callers can promote it consistently.
type PythonSemanticProfile struct {
	EntityType          string
	Decorators          []string
	Async               bool
	Generator           bool
	Lambda              bool
	Metaclass           string
	Docstring           string
	TypeAnnotation      bool
	TypeAnnotationCount int
	TypeAnnotationKinds []string
	AnnotationKind      string
	Context             string
}

// PythonSemanticProfileFromMetadata builds a promotion profile from a content
// entity type and its metadata payload.
func PythonSemanticProfileFromMetadata(entityType string, metadata map[string]any) PythonSemanticProfile {
	profile := PythonSemanticProfile{EntityType: entityType}
	if len(metadata) == 0 {
		return profile
	}

	profile.Decorators = stringSliceFromAny(metadata["decorators"])
	profile.Async = boolValue(metadata["async"])
	profile.Generator = metadataString(metadata, "semantic_kind") == "generator"
	profile.Lambda = metadataString(metadata, "semantic_kind") == "lambda"
	profile.Metaclass = metadataString(metadata, "metaclass")
	profile.Docstring = metadataString(metadata, "docstring")
	profile.TypeAnnotationCount = IntVal(metadata, "type_annotation_count")
	profile.TypeAnnotationKinds = stringSliceFromAny(metadata["type_annotation_kinds"])
	profile.AnnotationKind = metadataString(metadata, "annotation_kind")
	profile.Context = metadataString(metadata, "context")
	profile.TypeAnnotation = entityType == "TypeAnnotation" ||
		hasValues(metadata["type_annotations"]) ||
		profile.TypeAnnotationCount > 0 ||
		len(profile.TypeAnnotationKinds) > 0
	return profile
}

// HasSignals reports whether the profile contains any Python-specific signals.
func (p PythonSemanticProfile) HasSignals() bool {
	return len(p.Signals()) > 0
}

// Present reports whether the profile carries any Python-specific semantics.
func (p PythonSemanticProfile) Present() bool {
	return p.Docstring != "" || p.HasSignals()
}

// Fields returns the semantic fields as a promotion-ready map.
func (p PythonSemanticProfile) Fields() map[string]any {
	fields := make(map[string]any, 8)
	if len(p.Decorators) > 0 {
		fields["decorators"] = cloneStrings(p.Decorators)
	}
	if p.Async {
		fields["async"] = true
	}
	if p.Generator {
		fields["generator"] = true
	}
	if p.Lambda {
		fields["lambda"] = true
	}
	if p.Metaclass != "" {
		fields["metaclass"] = p.Metaclass
	}
	if p.Docstring != "" {
		fields["docstring"] = p.Docstring
	}
	if p.TypeAnnotationCount > 0 {
		fields["type_annotation_count"] = p.TypeAnnotationCount
	}
	if len(p.TypeAnnotationKinds) > 0 {
		fields["type_annotation_kinds"] = cloneStrings(p.TypeAnnotationKinds)
	}
	if p.AnnotationKind != "" {
		fields["annotation_kind"] = p.AnnotationKind
	}
	if p.Context != "" {
		fields["context"] = p.Context
	}
	if p.TypeAnnotation {
		fields["type_annotation"] = true
	}
	if surfaceKind := p.SurfaceKind(); surfaceKind != "" {
		fields["surface_kind"] = surfaceKind
	}
	if signals := p.Signals(); len(signals) > 0 {
		fields["signals"] = pythonSemanticSignalsToStrings(signals)
	}
	return fields
}

// AttachPythonSemantics returns a shallow copy of result with a dedicated
// python_semantics bundle when metadata contains promotable values.
func AttachPythonSemantics(result map[string]any, metadata map[string]any) map[string]any {
	if result == nil {
		result = map[string]any{}
	}

	semantics := PythonSemanticProfileFromMetadata("", metadata)
	if !semantics.Present() {
		return result
	}

	cloned := maps.Clone(result)
	cloned["python_semantics"] = semantics.Fields()
	return cloned
}

// PrimarySignal returns the highest-priority semantic signal present.
func (p PythonSemanticProfile) PrimarySignal() PythonSemanticSignal {
	signals := p.Signals()
	if len(signals) == 0 {
		return ""
	}
	return signals[0]
}

// Signals returns the profile signals in promotion priority order.
func (p PythonSemanticProfile) Signals() []PythonSemanticSignal {
	signals := make([]PythonSemanticSignal, 0, 6)
	if len(p.Decorators) > 0 {
		signals = append(signals, PythonSemanticSignalDecorator)
	}
	if p.Async {
		signals = append(signals, PythonSemanticSignalAsync)
	}
	if p.Generator {
		signals = append(signals, PythonSemanticSignalGenerator)
	}
	if p.Lambda {
		signals = append(signals, PythonSemanticSignalLambda)
	}
	if p.Metaclass != "" {
		signals = append(signals, PythonSemanticSignalMetaclass)
	}
	if p.Docstring != "" {
		signals = append(signals, PythonSemanticSignalDocstring)
	}
	if p.TypeAnnotation {
		signals = append(signals, PythonSemanticSignalTypeAnnotation)
	}
	if len(signals) == 0 {
		return nil
	}
	return signals
}

// SurfaceKind returns a small, stable label that the query layer can use when
// promoting Python semantics into story or context surfaces.
func (p PythonSemanticProfile) SurfaceKind() string {
	switch {
	case p.EntityType == "Class" && len(p.Decorators) > 0:
		return "decorated_class"
	case len(p.Decorators) > 0 && p.Async:
		return "decorated_async_function"
	case len(p.Decorators) > 0:
		return "decorated_function"
	case p.Async && p.Generator:
		return "async_generator_function"
	case p.Generator:
		return "generator_function"
	case p.Async:
		return "async_function"
	case p.Lambda:
		return "lambda_function"
	case p.EntityType == "Class" && p.Metaclass != "":
		return "metaclass_class"
	case p.TypeAnnotation && p.AnnotationKind == "parameter":
		return "parameter_type_annotation"
	case p.TypeAnnotation && p.AnnotationKind == "return":
		return "return_type_annotation"
	case p.TypeAnnotation:
		return "type_annotation"
	case p.EntityType == "Module" && p.Docstring != "":
		return "documented_module"
	case p.EntityType == "Class" && p.Docstring != "":
		return "documented_class"
	case p.EntityType == "Function" && p.Docstring != "":
		return "documented_function"
	case p.Docstring != "":
		return "documented_entity"
	default:
		return "plain"
	}
}

func pythonSemanticSignalsToStrings(signals []PythonSemanticSignal) []string {
	if len(signals) == 0 {
		return nil
	}

	values := make([]string, 0, len(signals))
	for _, signal := range signals {
		values = append(values, string(signal))
	}
	return values
}

func stringSliceFromAny(value any) []string {
	switch typed := value.(type) {
	case []string:
		return filterEmptyStrings(typed)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				continue
			}
			if trimmed := strings.TrimSpace(text); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	default:
		return nil
	}
}

func filterEmptyStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	items := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}

func boolValue(value any) bool {
	typed, ok := value.(bool)
	return ok && typed
}

func hasValues(value any) bool {
	switch typed := value.(type) {
	case []string:
		return len(filterEmptyStrings(typed)) > 0
	case []any:
		return len(typed) > 0
	default:
		return false
	}
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
