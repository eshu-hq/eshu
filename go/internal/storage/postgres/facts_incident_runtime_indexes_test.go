package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeIncidentRuntimeReadIndexes(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"fact_records_service_catalog_operational_link_url_idx",
		"fact_records_kubernetes_correlation_image_lookup_idx",
		"fact_kind = 'service_catalog.operational_link'",
		"fact_kind = 'reducer_kubernetes_correlation'",
		"(payload->>'source_digest')",
		"(payload->>'image_ref')",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("Bootstrap SQL missing incident runtime index fragment %q", want)
		}
	}
}
