package query

import "testing"

func TestSupportedLanguages_ExplicitJSXAndTSX(t *testing.T) {
	langs := SupportedLanguages()
	langSet := make(map[string]bool, len(langs))
	for _, lang := range langs {
		langSet[lang] = true
	}

	for _, want := range []string{"jsx", "tsx"} {
		if !langSet[want] {
			t.Fatalf("SupportedLanguages() missing %q in %#v", want, langs)
		}
	}
}

func TestBuildLanguageCypher_JSXUsesJavaScriptExtensions(t *testing.T) {
	cypher, params := buildLanguageCypher("jsx", "File", "Button", "", 5)

	if got, want := params["language"], "javascript"; got != want {
		t.Fatalf("params[language] = %#v, want %#v", got, want)
	}
	for _, fragment := range []string{".js", ".jsx", ".mjs", ".cjs"} {
		if !searchString(cypher, fragment) {
			t.Fatalf("buildLanguageCypher(\"jsx\") missing %q in %q", fragment, cypher)
		}
	}
}

func TestBuildLanguageCypher_TSXUsesTypeScriptExtensions(t *testing.T) {
	cypher, params := buildLanguageCypher("tsx", "File", "Component", "", 5)

	if got, want := params["language"], "typescript"; got != want {
		t.Fatalf("params[language] = %#v, want %#v", got, want)
	}
	for _, fragment := range []string{".ts", ".tsx"} {
		if !searchString(cypher, fragment) {
			t.Fatalf("buildLanguageCypher(\"tsx\") missing %q in %q", fragment, cypher)
		}
	}
}
