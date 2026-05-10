package rust

import "strings"

type rustImportEntry struct {
	name       string
	alias      string
	importType string
}

type rustGenericMetadata struct {
	lifetimes []string
	types     []string
	consts    []string
}

func rustApplyAttributeMetadata(item map[string]any, attributes []string) {
	if len(attributes) == 0 {
		return
	}
	item["decorators"] = attributes
	paths := make([]string, 0, len(attributes))
	derives := make([]string, 0)
	for _, attribute := range attributes {
		attrPath := rustAttributePath(attribute)
		if attrPath != "" {
			paths = appendUniqueString(paths, attrPath)
		}
		for _, derive := range rustDeriveNames(attribute) {
			derives = appendUniqueString(derives, derive)
		}
	}
	if len(paths) > 0 {
		item["attribute_paths"] = paths
	}
	if len(derives) > 0 {
		item["derives"] = derives
	}
}

// rustApplyRootKinds merges conservative root evidence without reordering prior facts.
func rustApplyRootKinds(item map[string]any, rootKinds []string) {
	if len(rootKinds) == 0 {
		return
	}
	existing, _ := item["dead_code_root_kinds"].([]string)
	for _, rootKind := range rootKinds {
		existing = appendUniqueString(existing, rootKind)
	}
	if len(existing) > 0 {
		item["dead_code_root_kinds"] = existing
	}
}

// rustApplyPublicAPIRootMetadata treats exact pub visibility as public API evidence.
func rustApplyPublicAPIRootMetadata(item map[string]any) {
	if item["visibility"] != "pub" {
		return
	}
	rustApplyRootKinds(item, []string{"rust.public_api_item"})
}

// rustHasBenchmarkAttribute accepts direct benchmark attributes and crate-qualified variants.
func rustHasBenchmarkAttribute(attributes []string) bool {
	for _, attribute := range attributes {
		attrPath := rustAttributePath(attribute)
		if attrPath == "bench" || strings.HasSuffix(attrPath, "::bench") {
			return true
		}
	}
	return false
}

func rustApplyGenericMetadata(item map[string]any, segment string) {
	metadata := rustParseGenericMetadata(segment)
	if len(metadata.lifetimes) > 0 {
		item["lifetime_parameters"] = metadata.lifetimes
	}
	if len(metadata.types) > 0 {
		item["type_parameters"] = metadata.types
	}
	if len(metadata.consts) > 0 {
		item["const_parameters"] = metadata.consts
	}
}

func rustGenericParametersAfterName(signature string, name string) string {
	trimmed := rustStripLeadingAttributeText(signature)
	if name == "" {
		return ""
	}
	idx := strings.Index(trimmed, name)
	if idx < 0 {
		return ""
	}
	remainder := strings.TrimSpace(trimmed[idx+len(name):])
	if !strings.HasPrefix(remainder, "<") {
		return ""
	}
	segment, ok := rustLeadingAngleSegment(remainder)
	if !ok {
		return ""
	}
	return segment
}

func rustLeadingGenericSegment(signature string) string {
	trimmed := strings.TrimSpace(signature)
	if !strings.HasPrefix(trimmed, "<") {
		return ""
	}
	segment, ok := rustLeadingAngleSegment(trimmed)
	if !ok {
		return ""
	}
	return segment
}

func rustParseGenericMetadata(segment string) rustGenericMetadata {
	segment = strings.TrimSpace(segment)
	segment = strings.TrimPrefix(segment, "<")
	segment = strings.TrimSuffix(segment, ">")
	if segment == "" {
		return rustGenericMetadata{}
	}
	var metadata rustGenericMetadata
	for _, part := range rustSplitTopLevel(segment, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch {
		case strings.HasPrefix(part, "'"):
			name := rustGenericName(part)
			if name != "" {
				metadata.lifetimes = appendUniqueString(metadata.lifetimes, name)
			}
		case strings.HasPrefix(part, "const "):
			name := rustGenericName(strings.TrimSpace(strings.TrimPrefix(part, "const ")))
			if name != "" {
				metadata.consts = appendUniqueString(metadata.consts, name)
			}
		default:
			name := rustGenericName(part)
			if name != "" {
				metadata.types = appendUniqueString(metadata.types, name)
			}
		}
	}
	return metadata
}

func rustGenericName(part string) string {
	part = strings.TrimSpace(part)
	part = strings.TrimPrefix(part, "'")
	for _, separator := range []string{":", "=", " "} {
		if idx := strings.Index(part, separator); idx >= 0 {
			part = part[:idx]
		}
	}
	return strings.TrimSpace(part)
}

func rustDeriveNames(attribute string) []string {
	if rustAttributePath(attribute) != "derive" {
		return nil
	}
	open := strings.Index(attribute, "(")
	close := strings.LastIndex(attribute, ")")
	if open < 0 || close <= open {
		return nil
	}
	names := make([]string, 0)
	for _, part := range rustSplitTopLevel(attribute[open+1:close], ',') {
		if name := strings.TrimSpace(part); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func rustImportEntries(importText string) []rustImportEntry {
	return rustExpandImport(strings.TrimSpace(importText))
}

func rustExpandImport(text string) []rustImportEntry {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if !strings.Contains(text, "{") {
		return []rustImportEntry{rustSingleImportEntry(text)}
	}
	prefix, body, ok := rustSplitBraceImport(text)
	if !ok {
		return []rustImportEntry{rustSingleImportEntry(text)}
	}
	entries := make([]rustImportEntry, 0)
	for _, part := range rustSplitTopLevel(body, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		switch part {
		case "self":
			entry := rustSingleImportEntry(prefix)
			entry.importType = "self"
			entry.alias = ""
			entries = append(entries, entry)
		default:
			if strings.HasPrefix(part, "self as ") {
				entry := rustSingleImportEntry(prefix + strings.TrimPrefix(part, "self"))
				entries = append(entries, entry)
				continue
			}
			childPrefix := prefix
			if childPrefix != "" {
				childPrefix += "::"
			}
			entries = append(entries, rustExpandImport(childPrefix+part)...)
		}
	}
	return entries
}

func rustSplitBraceImport(text string) (string, string, bool) {
	open := strings.Index(text, "{")
	if open < 0 {
		return "", "", false
	}
	depth := 0
	for idx := open; idx < len(text); idx++ {
		switch text[idx] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				prefix := strings.TrimSuffix(strings.TrimSpace(text[:open]), "::")
				body := text[open+1 : idx]
				return prefix, body, true
			}
		}
	}
	return "", "", false
}

func rustSingleImportEntry(text string) rustImportEntry {
	text = strings.TrimSpace(text)
	entry := rustImportEntry{name: text, alias: rustImportAlias(text), importType: "use"}
	if aliasIndex := strings.Index(text, " as "); aliasIndex >= 0 {
		entry.importType = "alias"
		entry.alias = strings.TrimSpace(text[aliasIndex+len(" as "):])
		entry.name = strings.TrimSpace(text[:aliasIndex])
		return entry
	}
	if strings.HasSuffix(text, "::*") {
		entry.importType = "glob"
		entry.alias = ""
		return entry
	}
	return entry
}

func rustModuleKind(raw string) string {
	if strings.Contains(raw, "{") {
		return "inline"
	}
	return "declaration"
}

// rustModuleDeclaredPathCandidates reports Rust's file-directory-relative mod lookup paths.
func rustModuleDeclaredPathCandidates(name string, moduleKind string) []string {
	if moduleKind != "declaration" {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	return []string{name + ".rs", name + "/mod.rs"}
}

// rustStripVisibility removes a parsed visibility prefix before import expansion.
func rustStripVisibility(text string, visibility string) string {
	trimmed := rustStripLeadingAttributeText(strings.TrimSpace(text))
	if visibility == "" {
		return trimmed
	}
	return strings.TrimSpace(strings.TrimPrefix(trimmed, visibility))
}

func rustStripLeadingAttributeText(text string) string {
	trimmed := strings.TrimSpace(text)
	for strings.HasPrefix(trimmed, "#[") {
		end := rustAttributeEnd(trimmed)
		if end < 0 {
			return trimmed
		}
		trimmed = strings.TrimSpace(trimmed[end+1:])
	}
	return trimmed
}

func rustAttributeEnd(text string) int {
	if !strings.HasPrefix(strings.TrimSpace(text), "#[") {
		return -1
	}
	bracketDepth := 0
	parenDepth := 0
	started := false
	for idx, r := range text {
		switch r {
		case '[':
			bracketDepth++
			started = true
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			if started && bracketDepth == 0 && parenDepth == 0 {
				return idx
			}
		case '(':
			if started {
				parenDepth++
			}
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		}
	}
	return -1
}

func rustSplitTopLevel(text string, separator rune) []string {
	parts := make([]string, 0)
	start := 0
	angleDepth := 0
	braceDepth := 0
	parenDepth := 0
	bracketDepth := 0
	for idx, r := range text {
		switch r {
		case '<':
			angleDepth++
		case '>':
			if angleDepth > 0 {
				angleDepth--
			}
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		default:
			if r == separator && angleDepth == 0 && braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				parts = append(parts, text[start:idx])
				start = idx + len(string(r))
			}
		}
	}
	parts = append(parts, text[start:])
	return parts
}
