package exposure

import (
	"strings"
	"testing"
)

// allowedSeverities is the closed severity vocabulary the catalog may use. It
// mirrors the lowercase normalized severity strings used elsewhere in the query
// surface (see internal/exports.Severity) so sink severities are wire-stable.
var allowedSeverities = map[Severity]struct{}{
	SeverityCritical: {},
	SeverityHigh:     {},
	SeverityMedium:   {},
	SeverityLow:      {},
}

// TestSinkCatalogIsWellFormed locks the curated cloud-sink catalog against the
// most likely authoring mistakes: an empty kind or display name, a duplicate
// (kind, relationship, target) recognition triple, a graph-backed spec missing
// its relationship/target/provenance, or a not-yet-materialized spec that
// secretly claims a relationship it cannot honor. The catalog is the security
// review focus, so a malformed entry is a correctness bug, not a style nit.
func TestSinkCatalogIsWellFormed(t *testing.T) {
	t.Parallel()

	type triple struct {
		kind   SinkKind
		rel    string
		target string
	}
	seen := make(map[triple]struct{})

	for _, spec := range SinkCatalog() {
		if strings.TrimSpace(string(spec.Kind)) == "" {
			t.Fatalf("catalog entry has empty kind: %+v", spec)
		}
		if strings.TrimSpace(spec.DisplayName) == "" {
			t.Fatalf("catalog entry %q has empty display name", spec.Kind)
		}
		if _, ok := allowedSeverities[spec.BaselineSeverity]; !ok {
			t.Fatalf("catalog entry %q has severity %q outside the closed vocabulary", spec.Kind, spec.BaselineSeverity)
		}

		key := triple{kind: spec.Kind, rel: spec.Relationship, target: spec.TargetLabel}
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate recognition triple for %q: rel=%q target=%q", spec.Kind, spec.Relationship, spec.TargetLabel)
		}
		seen[key] = struct{}{}

		for _, pred := range spec.TargetPredicates {
			if strings.TrimSpace(pred.Key) == "" {
				t.Fatalf("catalog entry %q has a predicate with an empty key", spec.Kind)
			}
		}

		if spec.GraphBacked {
			if strings.TrimSpace(spec.Relationship) == "" {
				t.Fatalf("graph-backed sink %q must name a relationship", spec.Kind)
			}
			if strings.TrimSpace(spec.TargetLabel) == "" {
				t.Fatalf("graph-backed sink %q must name a target label", spec.Kind)
			}
			if strings.TrimSpace(spec.Provenance) == "" {
				t.Fatalf("graph-backed sink %q must cite its provenance", spec.Kind)
			}
		} else {
			// A not-yet-materialized sink kind stays in the closed vocabulary but
			// must not pretend to recognize an edge; the tracer reports it
			// unresolved instead of fabricating a match.
			if strings.TrimSpace(spec.Relationship) != "" || strings.TrimSpace(spec.TargetLabel) != "" {
				t.Fatalf("not-yet-materialized sink %q must not declare a relationship/target", spec.Kind)
			}
			if strings.TrimSpace(spec.Provenance) == "" {
				t.Fatalf("not-yet-materialized sink %q must cite the follow-up that will materialize it", spec.Kind)
			}
		}
	}
}

// TestSinkCatalogCoversClosedVocabulary proves every SinkKind in the closed
// vocabulary has at least one catalog spec, so the five differentiating sink
// categories from #2704/#2724 are all represented.
func TestSinkCatalogCoversClosedVocabulary(t *testing.T) {
	t.Parallel()

	want := []SinkKind{
		SinkIAMPrivilegedAction,
		SinkSecretReference,
		SinkSQLTable,
		SinkShellExec,
		SinkInternetEndpoint,
	}
	covered := make(map[SinkKind]bool)
	for _, spec := range SinkCatalog() {
		covered[spec.Kind] = true
	}
	for _, kind := range want {
		if !covered[kind] {
			t.Fatalf("closed vocabulary kind %q has no catalog spec", kind)
		}
	}
}

// TestMatchSinkRecognizesFixtures exercises the recognizer against representative
// qualifying and non-qualifying edges. Positive cases mirror the real graph
// edges (CAN_PERFORM->CloudResource, QUERIES_TABLE->SqlTable, the internet
// SecurityGroupRule->CidrBlock edge, the secrets-read edge). Negative cases prove
// non-sinks (a plain CALLS edge, a non-internet CidrBlock, an unknown
// relationship) are rejected, so the tracer never invents a sink.
func TestMatchSinkRecognizesFixtures(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		rel       string
		target    string
		props     map[string]string
		wantKind  SinkKind
		wantMatch bool
	}{
		{name: "iam can_perform", rel: "CAN_PERFORM", target: "CloudResource", wantKind: SinkIAMPrivilegedAction, wantMatch: true},
		{name: "iam can_escalate_to", rel: "CAN_ESCALATE_TO", target: "CloudResource", wantKind: SinkIAMPrivilegedAction, wantMatch: true},
		{name: "iam can_assume", rel: "CAN_ASSUME", target: "CloudResource", wantKind: SinkIAMPrivilegedAction, wantMatch: true},
		{name: "secret read", rel: "SECRETS_IAM_GRANTS_SECRET_READ", target: "SecretsIAMSecretMetadataPath", wantKind: SinkSecretReference, wantMatch: true},
		{name: "internet cidr", rel: "TO", target: "CidrBlock", props: map[string]string{"is_internet": "true"}, wantKind: SinkInternetEndpoint, wantMatch: true},
		{name: "non-internet cidr rejected", rel: "TO", target: "CidrBlock", props: map[string]string{"is_internet": "false"}, wantMatch: false},
		{name: "cidr without flag rejected", rel: "TO", target: "CidrBlock", wantMatch: false},
		{name: "plain call rejected", rel: "CALLS", target: "Function", wantMatch: false},
		{name: "unknown relationship rejected", rel: "WAT", target: "CloudResource", wantMatch: false},
		{name: "sql queries_table", rel: "QUERIES_TABLE", target: "SqlTable", wantKind: SinkSQLTable, wantMatch: true},
		{name: "shell exec not graph-backed", rel: "EXECUTES_SHELL", target: "ShellCommand", wantMatch: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec, ok := MatchSink(tc.rel, tc.target, tc.props)
			if ok != tc.wantMatch {
				t.Fatalf("MatchSink(%q,%q,%v) match=%v, want %v", tc.rel, tc.target, tc.props, ok, tc.wantMatch)
			}
			if tc.wantMatch && spec.Kind != tc.wantKind {
				t.Fatalf("MatchSink(%q,%q) kind=%q, want %q", tc.rel, tc.target, spec.Kind, tc.wantKind)
			}
			if tc.wantMatch && !spec.GraphBacked {
				t.Fatalf("MatchSink returned a non-graph-backed spec for %q", tc.name)
			}
		})
	}
}

// TestNonGraphBackedSinksAreNeverMatched is the honesty-contract guard: every
// sink kind in the closed vocabulary that has no materialized graph fact must be
// excluded from GraphBackedSinkSpecs and must never be returned by MatchSink,
// even if a caller passes a relationship a future materializer might use. Today
// that is shell_exec (#2800).
func TestNonGraphBackedSinksAreNeverMatched(t *testing.T) {
	t.Parallel()

	graphBacked := make(map[SinkKind]bool)
	for _, spec := range GraphBackedSinkSpecs() {
		if !spec.GraphBacked {
			t.Fatalf("GraphBackedSinkSpecs returned a non-graph-backed kind %q", spec.Kind)
		}
		graphBacked[spec.Kind] = true
	}

	for _, spec := range SinkCatalog() {
		if spec.GraphBacked {
			continue
		}
		// A non-graph-backed spec must declare no recognition edge, so it cannot
		// be matched. Probing with any relationship/target must miss.
		if _, ok := MatchSink("EXECUTES_SHELL", "ShellCommand", nil); ok && spec.Kind == SinkShellExec {
			t.Fatalf("non-graph-backed sink %q was matched by MatchSink", spec.Kind)
		}
	}

	if !graphBacked[SinkSQLTable] {
		t.Fatal("sql_table must be graph-backed once QUERIES_TABLE is materialized")
	}
	if graphBacked[SinkShellExec] {
		t.Fatal("shell_exec must be non-graph-backed until #2800 materializes a command-execution fact")
	}
}

// TestSinkCatalogReturnsIsolatedPredicates proves SinkCatalog hands back a deep
// copy: mutating a returned spec's TargetPredicates must not corrupt the
// package-level catalog for the next caller.
func TestSinkCatalogReturnsIsolatedPredicates(t *testing.T) {
	t.Parallel()

	first := SinkCatalog()
	for i := range first {
		for j := range first[i].TargetPredicates {
			first[i].TargetPredicates[j].Value = "mutated"
		}
	}

	for _, spec := range SinkCatalog() {
		for _, pred := range spec.TargetPredicates {
			if pred.Value == "mutated" {
				t.Fatalf("SinkCatalog leaked a mutable predicate on %q", spec.Kind)
			}
		}
	}
}

// TestMatchSinkReturnsIsolatedPredicates proves MatchSink hands back a deep copy
// of the matched spec's predicates: mutating them must not corrupt the
// package-level catalog for the next caller (the internet endpoint spec is the
// one with predicates).
func TestMatchSinkReturnsIsolatedPredicates(t *testing.T) {
	t.Parallel()

	spec, ok := MatchSink("TO", "CidrBlock", map[string]string{"is_internet": "true"})
	if !ok {
		t.Fatal("expected the internet endpoint sink to match")
	}
	for i := range spec.TargetPredicates {
		spec.TargetPredicates[i].Value = "mutated"
	}
	// A fresh match must be unaffected.
	again, ok := MatchSink("TO", "CidrBlock", map[string]string{"is_internet": "true"})
	if !ok {
		t.Fatal("expected the internet endpoint sink to match again")
	}
	for _, pred := range again.TargetPredicates {
		if pred.Value == "mutated" {
			t.Fatal("MatchSink leaked a mutable predicate into the package catalog")
		}
	}
}

// TestSinkCatalogVersionIsStableAndChangeSensitive proves the content hash is
// deterministic across calls (so reachability results can cache against it) and
// changes when the catalog changes (the taintModelVersion discipline: a curated
// edit trips re-evaluation). The pinned golden forces a deliberate version bump
// whenever the catalog is edited.
func TestSinkCatalogVersionIsStableAndChangeSensitive(t *testing.T) {
	t.Parallel()

	v1 := SinkCatalogVersion()
	v2 := SinkCatalogVersion()
	if v1 != v2 {
		t.Fatalf("SinkCatalogVersion not deterministic: %q vs %q", v1, v2)
	}
	if v1 != sinkCatalogVersionGolden {
		t.Fatalf("SinkCatalogVersion = %q, want pinned golden %q; update the golden deliberately when the catalog changes", v1, sinkCatalogVersionGolden)
	}

	// Changing any field must change the hash.
	mutated := SinkCatalog()
	mutated[0].BaselineSeverity = SeverityLow
	if hashSinkSpecs(mutated) == v1 {
		t.Fatal("hashSinkSpecs is insensitive to a severity change")
	}
	extra := append(SinkCatalog(), SinkSpec{Kind: "synthetic", DisplayName: "synthetic", BaselineSeverity: SeverityLow, Provenance: "test"})
	if hashSinkSpecs(extra) == v1 {
		t.Fatal("hashSinkSpecs is insensitive to an added entry")
	}
}
