package java

import (
	"path/filepath"
	"testing"
)

// TestMetadataClassReferences pins the spring.factories multi-line continuation
// shape. The path is a bounded resource file — no tree-sitter Java grammar
// applies. Lines are read verbatim; metadataClassNamePattern is the only guard
// before a token becomes graph evidence.
func TestMetadataClassReferences(t *testing.T) {
	t.Parallel()

	path := filepath.Join("src", "main", "resources", "META-INF", "spring.factories")
	source := `# comment
org.springframework.boot.autoconfigure.EnableAutoConfiguration=\
com.example.FirstAutoConfiguration,\
com.example.SecondAutoConfiguration
`

	got := MetadataClassReferences(path, source)
	want := []ClassReference{
		{
			Name:       "FirstAutoConfiguration",
			FullName:   "com.example.FirstAutoConfiguration",
			LineNumber: 2,
			Kind:       "java.spring_autoconfiguration_class",
		},
		{
			Name:       "SecondAutoConfiguration",
			FullName:   "com.example.SecondAutoConfiguration",
			LineNumber: 2,
			Kind:       "java.spring_autoconfiguration_class",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("MetadataClassReferences() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("MetadataClassReferences()[%d] = %#v, want %#v", index, got[index], want[index])
		}
	}
}

// TestMetadataClassReferencesRejectsDynamicOrInvalidNames pins the rejection
// rules: hyphens, duplicates, and bare single-segment identifiers (no dot) are
// all dropped. Only the first valid, unique dotted class name survives.
func TestMetadataClassReferencesRejectsDynamicOrInvalidNames(t *testing.T) {
	t.Parallel()

	path := filepath.Join("META-INF", "services", "com.example.Plugin")
	source := `com.example.PluginImpl
not-a-java-class
com.example.PluginImpl
`

	got := MetadataClassReferences(path, source)
	want := []ClassReference{
		{
			Name:       "PluginImpl",
			FullName:   "com.example.PluginImpl",
			LineNumber: 1,
			Kind:       "java.service_loader_provider",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("MetadataClassReferences() len = %d, want %d: %#v", len(got), len(want), got)
	}
	if got[0] != want[0] {
		t.Fatalf("MetadataClassReferences()[0] = %#v, want %#v", got[0], want[0])
	}
}

// TestMetadataClassReferencesMetaInfServicesProvider pins the
// META-INF/services provider shape for a ServiceLoader file: one class per
// line, comments stripped, blanks ignored. The path classifier assigns the
// java.service_loader_provider kind and no tree-sitter grammar runs.
func TestMetadataClassReferencesMetaInfServicesProvider(t *testing.T) {
	t.Parallel()

	path := filepath.Join("META-INF", "services", "java.sql.Driver")
	source := `# JDBC driver implementations
com.acme.db.AcmeDriver
com.acme.db.LegacyDriver
# another comment
`

	got := MetadataClassReferences(path, source)
	want := []ClassReference{
		{
			Name:       "AcmeDriver",
			FullName:   "com.acme.db.AcmeDriver",
			LineNumber: 2,
			Kind:       "java.service_loader_provider",
		},
		{
			Name:       "LegacyDriver",
			FullName:   "com.acme.db.LegacyDriver",
			LineNumber: 3,
			Kind:       "java.service_loader_provider",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("MetadataClassReferences() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MetadataClassReferences()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

// TestMetadataClassReferencesSpringAutoconfigurationImports pins the
// spring/org.springframework.boot.autoconfigure.AutoConfiguration.imports
// shape: one class per line, deduplication enforced, no tree-sitter grammar.
func TestMetadataClassReferencesSpringAutoconfigurationImports(t *testing.T) {
	t.Parallel()

	path := filepath.Join(
		"META-INF", "spring",
		"org.springframework.boot.autoconfigure.autoconfiguration.imports",
	)
	source := `com.example.FooAutoConfiguration
com.example.BarAutoConfiguration
com.example.FooAutoConfiguration
`

	got := MetadataClassReferences(path, source)
	want := []ClassReference{
		{
			Name:       "FooAutoConfiguration",
			FullName:   "com.example.FooAutoConfiguration",
			LineNumber: 1,
			Kind:       "java.spring_autoconfiguration_class",
		},
		{
			Name:       "BarAutoConfiguration",
			FullName:   "com.example.BarAutoConfiguration",
			LineNumber: 2,
			Kind:       "java.spring_autoconfiguration_class",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("MetadataClassReferences() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MetadataClassReferences()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

// TestMetadataClassReferencesUnrecognizedPath pins that an unrecognized path
// (not META-INF/services and not a spring.factories or autoconfiguration file)
// returns nil, preventing arbitrary resource files from becoming graph evidence.
func TestMetadataClassReferencesUnrecognizedPath(t *testing.T) {
	t.Parallel()

	got := MetadataClassReferences("src/main/resources/application.properties", "foo.bar.Baz=some.Class")
	if got != nil {
		t.Fatalf("MetadataClassReferences() on unrecognized path = %#v, want nil", got)
	}
}

// TestMetadataClassReferencesRejectsBareSingleSegment pins that a bare,
// non-dotted identifier is rejected — the pattern requires at least one dot,
// ensuring that simple property keys, comments, or bare names in Spring
// properties files never become graph evidence.
func TestMetadataClassReferencesRejectsBareSingleSegment(t *testing.T) {
	t.Parallel()

	path := filepath.Join("META-INF", "services", "com.example.Spi")
	source := `com.example.ValidImpl
BareClassName
123invalid
com.example.$DollarOk
`

	got := MetadataClassReferences(path, source)
	want := []ClassReference{
		{
			Name:       "ValidImpl",
			FullName:   "com.example.ValidImpl",
			LineNumber: 1,
			Kind:       "java.service_loader_provider",
		},
		{
			Name:       "$DollarOk",
			FullName:   "com.example.$DollarOk",
			LineNumber: 4,
			Kind:       "java.service_loader_provider",
		},
	}
	if len(got) != len(want) {
		t.Fatalf("MetadataClassReferences() len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("MetadataClassReferences()[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}
