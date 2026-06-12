package reducer

func codeCallFunctionIsConstructor(item map[string]any) bool {
	name := anyToString(item["name"])
	classContext := codeCallClassContext(item["class_context"])
	if classContext == "" {
		return false
	}
	if name == "constructor" || name == "__init__" {
		return true
	}
	return name == classContext
}
