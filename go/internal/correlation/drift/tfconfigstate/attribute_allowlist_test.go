package tfconfigstate

import (
	"sort"
	"strings"
	"testing"
)

func TestAttributeAllowlistIsNonEmpty(t *testing.T) {
	t.Parallel()

	types := AllowlistResourceTypes()
	if len(types) == 0 {
		t.Fatal("AllowlistResourceTypes() is empty, want non-empty seed")
	}
	if len(types) < 5 {
		t.Fatalf("AllowlistResourceTypes() = %d, want >= 5 seed entries", len(types))
	}
}

func TestAttributeAllowlistResourceTypesAreSorted(t *testing.T) {
	t.Parallel()

	types := AllowlistResourceTypes()
	if !sort.StringsAreSorted(types) {
		t.Fatalf("AllowlistResourceTypes() = %v, want sorted", types)
	}
}

func TestAttributeAllowlistHasNoDuplicateAttributes(t *testing.T) {
	t.Parallel()

	for _, rt := range AllowlistResourceTypes() {
		attrs := AllowlistFor(rt)
		seen := map[string]struct{}{}
		for _, a := range attrs {
			if strings.TrimSpace(a) == "" {
				t.Fatalf("AllowlistFor(%q) contains a blank attribute", rt)
			}
			if _, ok := seen[a]; ok {
				t.Fatalf("AllowlistFor(%q) contains duplicate attribute %q", rt, a)
			}
			seen[a] = struct{}{}
		}
	}
}

func TestAllowlistForUnknownTypeReturnsNil(t *testing.T) {
	t.Parallel()

	if got := AllowlistFor("aws_made_up_thing"); got != nil {
		t.Fatalf("AllowlistFor(unknown) = %v, want nil", got)
	}
}

func TestAllowlistForReturnsCopy(t *testing.T) {
	t.Parallel()

	got := AllowlistFor("aws_s3_bucket")
	if len(got) == 0 {
		t.Fatal("AllowlistFor(aws_s3_bucket) = empty, want non-empty")
	}
	got[0] = "MUTATED"

	again := AllowlistFor("aws_s3_bucket")
	if again[0] == "MUTATED" {
		t.Fatal("AllowlistFor returned a shared slice; mutation leaked across calls")
	}
}
