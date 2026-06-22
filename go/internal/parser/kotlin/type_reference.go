package kotlin

import "strings"

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

// replaceWholeWord replaces every whole-word occurrence of old with new in
// value. A match is a run of old that is not flanked by identifier characters,
// so substituting a type parameter `T` does not corrupt `Type`.
func replaceWholeWord(value string, old string, new string) string {
	if value == "" || old == "" {
		return value
	}
	var builder strings.Builder
	for i := 0; i < len(value); {
		if hasWordAt(value, i, old) {
			builder.WriteString(new)
			i += len(old)
			continue
		}
		builder.WriteByte(value[i])
		i++
	}
	return builder.String()
}

// hasWordAt reports whether word occurs at index i in value bounded by
// non-identifier characters on both sides.
func hasWordAt(value string, i int, word string) bool {
	if i+len(word) > len(value) || value[i:i+len(word)] != word {
		return false
	}
	if i > 0 && isIdentifierByte(value[i-1]) {
		return false
	}
	if end := i + len(word); end < len(value) && isIdentifierByte(value[end]) {
		return false
	}
	return true
}

func isIdentifierByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
