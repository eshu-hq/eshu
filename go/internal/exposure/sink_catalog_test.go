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
		SinkConfigSecurityKey,
		SinkIaCMisconfiguration,
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

// TestConfigAndIaCSinksStayNonGraphBackedUntilFixpointPathExists records the
// #3191 positive catalog fixtures and the negative honesty contract: config and
// IaC sink kinds are allowed in the closed vocabulary, but they must not match a
// graph edge until a Function-anchored value-flow loader/materializer path
// exists for them.
func TestConfigAndIaCSinksStayNonGraphBackedUntilFixpointPathExists(t *testing.T) {
	t.Parallel()

	fixtures := map[SinkKind]struct {
		severity Severity
		text     string
	}{
		SinkConfigSecurityKey:   {severity: SeverityHigh, text: "security-relevant config key"},
		SinkIaCMisconfiguration: {severity: SeverityHigh, text: "IaC misconfiguration"},
	}

	catalog := SinkCatalog()
	for kind, fixture := range fixtures {
		spec, ok := sinkSpecByKind(catalog, kind)
		if !ok {
			t.Fatalf("catalog missing #3191 sink kind %q", kind)
		}
		if spec.GraphBacked {
			t.Fatalf("%q must stay non-graph-backed until the fixpoint loader has a Function-anchored path", kind)
		}
		if spec.Relationship != "" || spec.TargetLabel != "" {
			t.Fatalf("%q declared rel/target without graph backing: %+v", kind, spec)
		}
		if spec.BaselineSeverity != fixture.severity {
			t.Fatalf("%q severity = %q, want %q", kind, spec.BaselineSeverity, fixture.severity)
		}
		if !strings.Contains(spec.DisplayName, fixture.text) {
			t.Fatalf("%q display name %q does not record the fixture intent %q", kind, spec.DisplayName, fixture.text)
		}
		if !strings.Contains(spec.Provenance, "#3191") || !strings.Contains(spec.Provenance, "non-GraphBacked") {
			t.Fatalf("%q provenance must cite #3191 and non-GraphBacked status: %q", kind, spec.Provenance)
		}
	}

	negativeEdges := []struct {
		name   string
		rel    string
		target string
		props  map[string]string
	}{
		{
			name:   "config unsafe tls verify stays unmatched",
			rel:    "WRITES_CONFIG",
			target: "ConfigKey",
			props:  map[string]string{"key": "tls.insecure_skip_verify", "unsafe_value": "true"},
		},
		{
			name:   "iac public bucket stays unmatched",
			rel:    "DECLARES_IAC_MISCONFIG",
			target: "TerraformResource",
			props:  map[string]string{"resource_type": "aws_s3_bucket_acl", "acl": "public-read"},
		},
	}
	for _, edge := range negativeEdges {
		if spec, ok := MatchSink(edge.rel, edge.target, edge.props); ok {
			t.Fatalf("%s matched catalog-only sink %+v; non-GraphBacked specs must not fabricate matches", edge.name, spec)
		}
	}
}

func sinkSpecByKind(specs []SinkSpec, kind SinkKind) (SinkSpec, bool) {
	for _, spec := range specs {
		if spec.Kind == kind {
			return spec, true
		}
	}
	return SinkSpec{}, false
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
		{name: "shell executes_shell", rel: "EXECUTES_SHELL", target: "ShellCommand", wantKind: SinkShellExec, wantMatch: true},
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

// TestGraphBackedSinksAreMatched is the honesty-contract guard: once a sink kind
// has a materialized graph edge, it must be returned by GraphBackedSinkSpecs so
// the tracer can resolve real paths rather than reporting a stale follow-up.
func TestGraphBackedSinksAreMatched(t *testing.T) {
	t.Parallel()

	graphBacked := make(map[SinkKind]bool)
	for _, spec := range GraphBackedSinkSpecs() {
		if !spec.GraphBacked {
			t.Fatalf("GraphBackedSinkSpecs returned a non-graph-backed kind %q", spec.Kind)
		}
		graphBacked[spec.Kind] = true
	}

	if !graphBacked[SinkSQLTable] {
		t.Fatal("sql_table must be graph-backed once QUERIES_TABLE is materialized")
	}
	if !graphBacked[SinkShellExec] {
		t.Fatal("shell_exec must be graph-backed once EXECUTES_SHELL is materialized")
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
