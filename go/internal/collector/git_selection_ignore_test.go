package collector

import "testing"

func TestParseCollectorGitignoreSpecClassifiesLiteralPatterns(t *testing.T) {
	t.Parallel()

	spec := parseCollectorGitignoreSpec([]string{
		"dist",
		"src/generated",
		"*.log",
	})
	if spec == nil {
		t.Fatal("parseCollectorGitignoreSpec() = nil, want patterns")
	}
	if got := len(spec.patterns); got != 3 {
		t.Fatalf("len(patterns) = %d, want 3", got)
	}
	if spec.patterns[0].hasGlob {
		t.Fatalf("patterns[0].hasGlob = true, want false for literal basename pattern")
	}
	if spec.patterns[1].hasGlob {
		t.Fatalf("patterns[1].hasGlob = true, want false for literal path pattern")
	}
	if !spec.patterns[2].hasGlob {
		t.Fatalf("patterns[2].hasGlob = false, want true for wildcard pattern")
	}
}

func BenchmarkCollectorGitignoreLiteralPatternMatch(b *testing.B) {
	spec := parseCollectorGitignoreSpec([]string{
		"dist",
		"node_modules",
		"build/generated",
		"*.tmp",
	})
	if spec == nil {
		b.Fatal("parseCollectorGitignoreSpec() = nil, want patterns")
	}
	rel := "services/api/build/generated/client/index.ts"

	b.ReportAllocs()
	for b.Loop() {
		for _, pattern := range spec.patterns {
			_ = pattern.matches(rel)
		}
	}
}
