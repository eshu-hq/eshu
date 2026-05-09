package kotlin

import (
	"regexp"
	"strings"
)

func kotlinCanonicalTypeReference(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	return strings.TrimSuffix(trimmed, "?")
}

func kotlinBaseTypeName(value string) string {
	normalized := kotlinCanonicalTypeReference(value)
	if normalized == "" {
		return ""
	}
	if index := strings.Index(normalized, "<"); index >= 0 {
		normalized = normalized[:index]
	}
	return strings.TrimSpace(normalized)
}

func kotlinTypeArguments(value string) []string {
	normalized := kotlinCanonicalTypeReference(value)
	if normalized == "" {
		return nil
	}
	start := strings.Index(normalized, "<")
	if start < 0 {
		return nil
	}
	end := strings.LastIndex(normalized, ">")
	if end <= start {
		return nil
	}

	spec := normalized[start+1 : end]
	parts := make([]string, 0, 2)
	depth := 0
	last := 0
	for index, char := range spec {
		switch char {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(spec[last:index])
				if part != "" {
					parts = append(parts, part)
				}
				last = index + 1
			}
		}
	}
	if part := strings.TrimSpace(spec[last:]); part != "" {
		parts = append(parts, part)
	}
	return parts
}

func kotlinResolveTypeReference(typeReference string, receiverType string, classTypeParameters map[string][]string) string {
	normalized := kotlinCanonicalTypeReference(typeReference)
	if normalized == "" {
		return ""
	}

	baseType := kotlinBaseTypeName(receiverType)
	if baseType == "" {
		return normalized
	}
	typeParameters := classTypeParameters[baseType]
	if len(typeParameters) == 0 {
		return normalized
	}

	typeArguments := kotlinTypeArguments(receiverType)
	if len(typeArguments) == 0 {
		return normalized
	}

	resolved := normalized
	for index, typeParameter := range typeParameters {
		if index >= len(typeArguments) || typeParameter == "" {
			continue
		}
		resolved = replaceWholeWord(resolved, typeParameter, typeArguments[index])
	}
	return strings.TrimSpace(resolved)
}

func replaceWholeWord(value string, old string, new string) string {
	if value == "" || old == "" {
		return value
	}
	pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(old) + `\b`)
	return pattern.ReplaceAllString(value, new)
}

func kotlinDeclaredTypeParameters(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}

	keywordIndex := strings.Index(trimmed, "class ")
	if keywordIndex < 0 {
		keywordIndex = strings.Index(trimmed, "interface ")
	}
	if keywordIndex < 0 {
		return nil
	}

	section := trimmed[keywordIndex:]
	start := strings.Index(section, "<")
	if start < 0 {
		return nil
	}
	end := strings.Index(section[start+1:], ">")
	if end < 0 {
		return nil
	}
	spec := section[start+1 : start+1+end]
	if strings.TrimSpace(spec) == "" {
		return nil
	}

	typeParameters := make([]string, 0, 4)
	depth := 0
	last := 0
	appendParameter := func(segment string) {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return
		}
		if index := strings.Index(segment, ":"); index >= 0 {
			segment = strings.TrimSpace(segment[:index])
		}
		if index := strings.Index(segment, " "); index >= 0 {
			segment = strings.TrimSpace(segment[:index])
		}
		if index := strings.Index(segment, "where"); index >= 0 {
			segment = strings.TrimSpace(segment[:index])
		}
		if segment != "" {
			typeParameters = append(typeParameters, segment)
		}
	}

	for index, char := range spec {
		switch char {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				appendParameter(spec[last:index])
				last = index + 1
			}
		}
	}
	appendParameter(spec[last:])
	if len(typeParameters) == 0 {
		return nil
	}
	return typeParameters
}
