package collector

import "testing"

func TestLoadDiscoveryOptionsFromEnvParsesIgnoredAndPreservedGlobs(t *testing.T) {
	t.Parallel()

	opts, err := LoadDiscoveryOptionsFromEnv(func(key string) string {
		switch key {
		case "ESHU_DISCOVERY_IGNORED_PATH_GLOBS":
			return "generated/**=generated-template, fixtures/*.json = fixture-data\narchive/**"
		case "ESHU_DISCOVERY_PRESERVED_PATH_GLOBS":
			return "generated/keep/**, fixtures/contract.json\n"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("LoadDiscoveryOptionsFromEnv() error = %v", err)
	}

	if got, want := len(opts.IgnoredPathGlobs), 3; got != want {
		t.Fatalf("IgnoredPathGlobs length = %d, want %d", got, want)
	}
	if got, want := opts.IgnoredPathGlobs[0].Pattern, "generated/**"; got != want {
		t.Fatalf("IgnoredPathGlobs[0].Pattern = %q, want %q", got, want)
	}
	if got, want := opts.IgnoredPathGlobs[0].Reason, "generated-template"; got != want {
		t.Fatalf("IgnoredPathGlobs[0].Reason = %q, want %q", got, want)
	}
	if got, want := opts.IgnoredPathGlobs[2].Reason, "env-ignore"; got != want {
		t.Fatalf("IgnoredPathGlobs[2].Reason = %q, want %q", got, want)
	}
	if got, want := opts.PreservedPathGlobs, []string{"generated/keep/**", "fixtures/contract.json"}; !sameStrings(got, want) {
		t.Fatalf("PreservedPathGlobs = %#v, want %#v", got, want)
	}
}

func TestLoadDiscoveryOptionsFromEnvRejectsInvalidIgnoredRule(t *testing.T) {
	t.Parallel()

	_, err := LoadDiscoveryOptionsFromEnv(func(key string) string {
		if key == "ESHU_DISCOVERY_IGNORED_PATH_GLOBS" {
			return "=missing-pattern"
		}
		return ""
	})
	if err == nil {
		t.Fatal("LoadDiscoveryOptionsFromEnv() error = nil, want invalid rule error")
	}
}

func sameStrings(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
