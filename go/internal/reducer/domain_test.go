package reducer

import (
	"sort"
	"testing"
)

// TestAllDomainsMatchesKnownDomains locks AllDomains to the knownDomains
// registry so a domain added to one without the other is caught. The surface
// inventory drift gate (#3145) enumerates reducer domains through AllDomains, so
// a new domain that skips the registry would silently leave the inventory.
func TestAllDomainsMatchesKnownDomains(t *testing.T) {
	t.Parallel()
	got := AllDomains()
	if len(got) != len(knownDomains) {
		t.Fatalf("AllDomains() returned %d domains, knownDomains has %d", len(got), len(knownDomains))
	}
	seen := map[Domain]bool{}
	for _, d := range got {
		if _, ok := knownDomains[d]; !ok {
			t.Errorf("AllDomains() returned %q which is not a known domain", d)
		}
		if seen[d] {
			t.Errorf("AllDomains() returned duplicate domain %q", d)
		}
		seen[d] = true
		if err := d.Validate(); err != nil {
			t.Errorf("AllDomains() returned invalid domain %q: %v", d, err)
		}
	}
}

// TestAllDomainsIsSorted guarantees deterministic order for generated output.
func TestAllDomainsIsSorted(t *testing.T) {
	t.Parallel()
	got := AllDomains()
	if !sort.SliceIsSorted(got, func(i, j int) bool { return got[i] < got[j] }) {
		t.Fatalf("AllDomains() is not sorted: %v", got)
	}
}
