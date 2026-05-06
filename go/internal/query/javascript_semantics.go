package query

import "maps"

// JavaScriptSemantics captures the JavaScript-specific metadata that the query
// layer promotes into first-class shared result surfaces such as
// semantic_profile and javascript_semantics.
type JavaScriptSemantics struct {
	Docstring  string
	MethodKind string
}

// ExtractJavaScriptSemantics returns the JavaScript semantics present in a
// content metadata map.
func ExtractJavaScriptSemantics(metadata map[string]any) JavaScriptSemantics {
	if len(metadata) == 0 {
		return JavaScriptSemantics{}
	}

	return JavaScriptSemantics{
		Docstring:  metadataString(metadata, "docstring"),
		MethodKind: metadataString(metadata, "method_kind"),
	}
}

// Present reports whether any JavaScript-specific semantics were found.
func (s JavaScriptSemantics) Present() bool {
	return s.Docstring != "" || s.MethodKind != ""
}

// Fields returns the semantic fields as a promotion-ready map.
func (s JavaScriptSemantics) Fields() map[string]any {
	fields := make(map[string]any, 2)
	if s.Docstring != "" {
		fields["docstring"] = s.Docstring
	}
	if s.MethodKind != "" {
		fields["method_kind"] = s.MethodKind
	}
	return fields
}

// AttachJavaScriptSemantics returns a shallow copy of result with a dedicated
// javascript_semantics bundle when metadata contains promotable values.
func AttachJavaScriptSemantics(result map[string]any, metadata map[string]any) map[string]any {
	if result == nil {
		result = map[string]any{}
	}

	semantics := ExtractJavaScriptSemantics(metadata)
	if !semantics.Present() {
		return result
	}

	cloned := maps.Clone(result)
	cloned["javascript_semantics"] = semantics.Fields()
	return cloned
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
