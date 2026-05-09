package javascript

import "strings"

func javaScriptTypeParameterNames(declaration string) []string {
	section, ok := javaScriptDelimitedSection(declaration, '<', '>')
	if !ok {
		return []string{}
	}

	parts := javaScriptSplitTopLevelSections(section)
	typeParameters := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		fields := strings.Fields(normalized)
		if len(fields) == 0 {
			continue
		}
		typeParameters = append(typeParameters, fields[0])
	}
	return typeParameters
}

func javaScriptDelimitedSection(text string, open, close byte) (string, bool) {
	start := strings.IndexByte(text, open)
	if start < 0 {
		return "", false
	}

	depth := 0
	for index := start; index < len(text); index++ {
		switch text[index] {
		case open:
			depth++
		case close:
			if depth == 0 {
				return "", false
			}
			depth--
			if depth == 0 {
				return text[start+1 : index], true
			}
		}
	}
	return "", false
}

func javaScriptSplitTopLevelSections(text string) []string {
	sections := make([]string, 0)
	start := 0
	depthAngles := 0
	depthParens := 0
	depthBraces := 0
	depthBrackets := 0

	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '<':
			depthAngles++
		case '>':
			if depthAngles > 0 {
				depthAngles--
			}
		case '(':
			depthParens++
		case ')':
			if depthParens > 0 {
				depthParens--
			}
		case '{':
			depthBraces++
		case '}':
			if depthBraces > 0 {
				depthBraces--
			}
		case '[':
			depthBrackets++
		case ']':
			if depthBrackets > 0 {
				depthBrackets--
			}
		case ',':
			if depthAngles == 0 && depthParens == 0 && depthBraces == 0 && depthBrackets == 0 {
				sections = append(sections, text[start:index])
				start = index + 1
			}
		}
	}

	sections = append(sections, text[start:])
	return sections
}
