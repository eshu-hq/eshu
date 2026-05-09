package java

import (
	"path/filepath"
	"testing"
)

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
