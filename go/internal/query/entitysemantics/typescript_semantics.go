// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitysemantics

import (
	"fmt"
	"maps"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TypeScriptSemanticProfile captures TypeScript and TSX metadata that the
// query layer promotes into first-class shared result surfaces. The
// implementation moved from root's typescript_semantics.go for #6060 so a
// handler-family subpackage can build the same profile without importing
// root.
type TypeScriptSemanticProfile struct {
	Decorators             []string
	TypeParameters         []string
	TypeAliasKind          string
	DeclarationMergeGroup  string
	DeclarationMergeCount  int
	DeclarationMergeKinds  []string
	ComponentTypeAssertion string
	ComponentWrapperKind   string
	JSXFragmentShorthand   bool
}

// TypeScriptSemanticProfileFromMetadata builds a promotion profile from a
// content entity metadata payload.
func TypeScriptSemanticProfileFromMetadata(metadata map[string]any) TypeScriptSemanticProfile {
	if len(metadata) == 0 {
		return TypeScriptSemanticProfile{}
	}

	return TypeScriptSemanticProfile{
		Decorators:             querycontract.MetadataStringSlice(metadata, "decorators"),
		TypeParameters:         querycontract.MetadataStringSlice(metadata, "type_parameters"),
		TypeAliasKind:          querycontract.MetadataString(metadata, "type_alias_kind"),
		DeclarationMergeGroup:  querycontract.MetadataString(metadata, "declaration_merge_group"),
		DeclarationMergeCount:  querycontract.IntVal(metadata, "declaration_merge_count"),
		DeclarationMergeKinds:  querycontract.MetadataStringSlice(metadata, "declaration_merge_kinds"),
		ComponentTypeAssertion: querycontract.MetadataString(metadata, "component_type_assertion"),
		ComponentWrapperKind:   querycontract.MetadataString(metadata, "component_wrapper_kind"),
		JSXFragmentShorthand:   querycontract.BoolValue(metadata["jsx_fragment_shorthand"]),
	}
}

// Present reports whether any TypeScript-specific semantics were found.
func (p TypeScriptSemanticProfile) Present() bool {
	return len(p.Decorators) > 0 ||
		len(p.TypeParameters) > 0 ||
		p.TypeAliasKind != "" ||
		p.DeclarationMergeGroup != "" ||
		p.DeclarationMergeCount > 0 ||
		len(p.DeclarationMergeKinds) > 0 ||
		p.ComponentTypeAssertion != "" ||
		p.ComponentWrapperKind != "" ||
		p.JSXFragmentShorthand
}

// Fields returns the semantic fields as a promotion-ready map.
func (p TypeScriptSemanticProfile) Fields() map[string]any {
	fields := make(map[string]any, 8)
	if len(p.Decorators) > 0 {
		fields["decorators"] = querycontract.CloneStrings(p.Decorators)
	}
	if len(p.TypeParameters) > 0 {
		fields["type_parameters"] = querycontract.CloneStrings(p.TypeParameters)
	}
	if p.TypeAliasKind != "" {
		fields["type_alias_kind"] = p.TypeAliasKind
	}
	if p.DeclarationMergeGroup != "" {
		fields["declaration_merge_group"] = p.DeclarationMergeGroup
	}
	if p.DeclarationMergeCount > 0 {
		fields["declaration_merge_count"] = p.DeclarationMergeCount
	}
	if len(p.DeclarationMergeKinds) > 0 {
		fields["declaration_merge_kinds"] = querycontract.CloneStrings(p.DeclarationMergeKinds)
	}
	if p.ComponentTypeAssertion != "" {
		fields["component_type_assertion"] = p.ComponentTypeAssertion
	}
	if p.ComponentWrapperKind != "" {
		fields["component_wrapper_kind"] = p.ComponentWrapperKind
	}
	if p.JSXFragmentShorthand {
		fields["jsx_fragment_shorthand"] = true
	}
	return fields
}

// AttachTypeScriptSemantics returns a shallow copy of result with a dedicated
// typescript_semantics bundle when metadata contains promotable values.
func AttachTypeScriptSemantics(result map[string]any, metadata map[string]any) map[string]any {
	if result == nil {
		result = map[string]any{}
	}

	semantics := TypeScriptSemanticProfileFromMetadata(metadata)
	if !semantics.Present() {
		return result
	}

	cloned := maps.Clone(result)
	cloned["typescript_semantics"] = semantics.Fields()
	return cloned
}

func typeScriptDeclarationMergeSummary(label string, name string, metadata map[string]any) string {
	group, _ := metadata["declaration_merge_group"].(string)
	if group == "" || name == "" {
		return ""
	}

	count := querycontract.IntVal(metadata, "declaration_merge_count")
	if count < 2 {
		return ""
	}

	kinds := querycontract.MetadataStringSlice(metadata, "declaration_merge_kinds")
	switch {
	case len(kinds) == 1 && kinds[0] == "interface":
		return fmt.Sprintf("%s %s participates in TypeScript declaration merging with %d same-name interface declarations.", label, name, count)
	case count == 2:
		if partner := declarationMergePartnerKind(label, kinds); partner != "" {
			return fmt.Sprintf("%s %s participates in TypeScript declaration merging with %s %s.", label, name, partner, group)
		}
	}

	return fmt.Sprintf("%s %s participates in TypeScript declaration merging with %d declarations.", label, name, count)
}

func declarationMergePartnerKind(label string, kinds []string) string {
	currentKind := declarationMergeEntityKind(label)
	for _, kind := range kinds {
		if kind == "" || kind == currentKind {
			continue
		}
		return kind
	}
	return ""
}

func declarationMergeEntityKind(label string) string {
	switch label {
	case "Class":
		return "class"
	case "Function":
		return "function"
	case "Interface":
		return "interface"
	case "Enum":
		return "enum"
	case "Module":
		return "namespace"
	default:
		return ""
	}
}
