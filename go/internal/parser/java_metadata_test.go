package parser

import (
	"path/filepath"
	"testing"
)

func TestDefaultRegistryLookupByPathJavaMetadata(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	paths := []string{
		filepath.Join("META-INF", "services", "com.example.Plugin"),
		filepath.Join("src", "main", "resources", "META-INF", "services", "com.example.Plugin"),
		filepath.Join("src", "main", "resources", "META-INF", "spring", "org.springframework.boot.autoconfigure.AutoConfiguration.imports"),
		filepath.Join("META-INF", "spring.factories"),
		filepath.Join("src", "main", "resources", "META-INF", "spring.factories"),
	}
	for _, path := range paths {
		definition, ok := registry.LookupByPath(path)
		if !ok {
			t.Fatalf("LookupByPath(%q) ok = false, want true", path)
		}
		if got, want := definition.Language, "java_metadata"; got != want {
			t.Fatalf("LookupByPath(%q).Language = %q, want %q", path, got, want)
		}
	}
}

func TestDefaultEngineParsePathJavaMetadataEmitsStaticClassReferences(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	servicesPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "services", "com.example.Plugin")
	writeTestFile(t, servicesPath, `# service implementations
com.example.PluginImpl
com.example.PluginImpl # duplicate
`)
	importsPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "spring", "org.springframework.boot.autoconfigure.AutoConfiguration.imports")
	writeTestFile(t, importsPath, `com.example.AutoConfig
`)
	factoriesPath := filepath.Join(repoRoot, "src", "main", "resources", "META-INF", "spring.factories")
	writeTestFile(t, factoriesPath, `org.springframework.boot.autoconfigure.EnableAutoConfiguration=\
com.example.LegacyAutoConfig,\
com.example.MoreAutoConfig
`)

	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	for _, tc := range []struct {
		path string
		name string
		kind string
		full string
	}{
		{
			path: servicesPath,
			name: "PluginImpl",
			kind: "java.service_loader_provider",
			full: "com.example.PluginImpl",
		},
		{
			path: importsPath,
			name: "AutoConfig",
			kind: "java.spring_autoconfiguration_class",
			full: "com.example.AutoConfig",
		},
		{
			path: factoriesPath,
			name: "LegacyAutoConfig",
			kind: "java.spring_autoconfiguration_class",
			full: "com.example.LegacyAutoConfig",
		},
		{
			path: factoriesPath,
			name: "MoreAutoConfig",
			kind: "java.spring_autoconfiguration_class",
			full: "com.example.MoreAutoConfig",
		},
	} {
		got, err := engine.ParsePath(repoRoot, tc.path, false, Options{})
		if err != nil {
			t.Fatalf("ParsePath(%q) error = %v, want nil", tc.path, err)
		}
		if gotLang, wantLang := got["lang"], "java_metadata"; gotLang != wantLang {
			t.Fatalf("ParsePath(%q) lang = %#v, want %#v", tc.path, gotLang, wantLang)
		}
		ref := assertJavaFunctionCallByNameAndKind(t, got, tc.name, tc.kind)
		assertStringFieldValue(t, ref, "full_name", tc.full)
		assertStringFieldValue(t, ref, "referenced_class", tc.full)
	}
}
