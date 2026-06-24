package query

import (
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestDocumentationHandlerDisclosesDeniedFindingOnWire proves the approved
// disclosure markers reach the HTTP wire end to end (#2164): a denied finding is
// returned in the findings list with access_disposition=access_denied,
// permission_denied + content_withheld set, and its protected content stripped.
// Because the MCP server proxies this exact route and returns the API response
// body verbatim, this is also the API+MCP envelope-parity contract.
func TestDocumentationHandlerDisclosesDeniedFindingOnWire(t *testing.T) {
	t.Parallel()

	deniedFinding := []byte(`{
		"finding_id": "finding:wire-denied",
		"finding_type": "service_deployment_drift",
		"status": "conflict",
		"freshness_state": "fresh",
		"summary": "PROTECTED wire content",
		"permissions": {"viewer_can_read_source": true},
		"acl_summary": {"source_acl_state": "denied"}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{deniedFinding}},
	}})
	handler := &DocumentationHandler{Content: NewContentReader(db), Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/documentation/findings", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	findings := resp["findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1 (denied disclosed, not dropped)", len(findings))
	}
	finding := findings[0].(map[string]any)
	if finding["access_disposition"] != "access_denied" {
		t.Fatalf("access_disposition = %#v, want access_denied", finding["access_disposition"])
	}
	if finding["permission_denied"] != true || finding["content_withheld"] != true {
		t.Fatalf("denied markers wrong on wire: %#v", finding)
	}
	if finding["source_acl_state"] != "denied" {
		t.Fatalf("source_acl_state = %#v, want denied", finding["source_acl_state"])
	}
	if _, leaked := finding["summary"]; leaked {
		t.Fatalf("denied finding leaked content on wire: %#v", finding)
	}
	if finding["freshness_state"] != "fresh" {
		t.Fatalf("freshness_state collapsed on wire: %#v", finding)
	}
}

// TestDocumentationFindingsEnforcementProofMatrix proves the full #2164 approved
// disclosure matrix on the findings list read model: allowed -> visible,
// denied -> access-denied + content withheld + permission_denied, partial ->
// partial marker + content withheld (no permission_denied), stale -> surfaced
// as stale with content intact, missing -> disclosed as missing with content
// intact-but-empty semantics. Every bounded posture here has a clean binary
// read decision so the bounded source_acl_state is the sole driver, and the
// existing freshness/truth labels (#2138) are preserved on every row.
func TestDocumentationFindingsEnforcementProofMatrix(t *testing.T) {
	t.Parallel()

	row := func(id, state string) []byte {
		return []byte(`{
			"finding_id": "finding:` + id + `",
			"finding_type": "service_deployment_drift",
			"status": "conflict",
			"truth_level": "derived",
			"freshness_state": "fresh",
			"summary": "PROTECTED ` + id + ` content",
			"permissions": {"viewer_can_read_source": true},
			"acl_summary": {"source_acl_state": "` + state + `"}
		}`)
	}
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows: [][]driver.Value{
			{row("allowed", facts.SourceACLStateAllowed)},
			{row("denied", facts.SourceACLStateDenied)},
			{row("partial", facts.SourceACLStatePartial)},
			{row("stale", facts.SourceACLStateStale)},
			{row("missing", facts.SourceACLStateMissing)},
		},
	}})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{Limit: 50})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	if len(got.Findings) != 5 {
		t.Fatalf("len(Findings) = %d, want 5 (no row dropped); findings = %#v", len(got.Findings), got.Findings)
	}
	byID := map[string]map[string]any{}
	for _, f := range got.Findings {
		byID[f["finding_id"].(string)] = f
	}

	// allowed -> visible, content intact.
	allowed := byID["finding:allowed"]
	if allowed[accessDispositionResponseKey] != accessDispositionVisible {
		t.Fatalf("allowed disposition = %#v, want %q", allowed[accessDispositionResponseKey], accessDispositionVisible)
	}
	if allowed["summary"] != "PROTECTED allowed content" {
		t.Fatalf("allowed row content withheld unexpectedly: %#v", allowed)
	}

	// denied -> access-denied, content withheld, permission_denied, #2138 preserved.
	denied := byID["finding:denied"]
	if denied[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("denied disposition = %#v, want %q", denied[accessDispositionResponseKey], accessDispositionDenied)
	}
	if denied[permissionDeniedResponseKey] != true || denied[contentWithheldResponseKey] != true {
		t.Fatalf("denied markers wrong: %#v", denied)
	}
	if _, leaked := denied["summary"]; leaked {
		t.Fatalf("denied row leaked content: %#v", denied)
	}
	if denied["freshness_state"] != "fresh" || denied["truth_level"] != "derived" {
		t.Fatalf("#2138 labels collapsed on denied row: %#v", denied)
	}
	if denied["source_acl_state"] != facts.SourceACLStateDenied {
		t.Fatalf("denied source_acl_state = %#v", denied["source_acl_state"])
	}

	// partial -> partial marker, content withheld, NOT permission_denied.
	partial := byID["finding:partial"]
	if partial[accessDispositionResponseKey] != accessDispositionPartial {
		t.Fatalf("partial disposition = %#v, want %q", partial[accessDispositionResponseKey], accessDispositionPartial)
	}
	if partial[contentWithheldResponseKey] != true {
		t.Fatalf("partial must withhold content: %#v", partial)
	}
	if _, denial := partial[permissionDeniedResponseKey]; denial {
		t.Fatalf("partial must not set permission_denied: %#v", partial)
	}
	if _, leaked := partial["summary"]; leaked {
		t.Fatalf("partial row leaked content: %#v", partial)
	}

	// stale -> surfaced as stale, content intact (permitted-but-stale).
	stale := byID["finding:stale"]
	if stale[accessDispositionResponseKey] != accessDispositionStale {
		t.Fatalf("stale disposition = %#v, want %q", stale[accessDispositionResponseKey], accessDispositionStale)
	}
	if stale["summary"] != "PROTECTED stale content" {
		t.Fatalf("stale row withheld content unexpectedly: %#v", stale)
	}

	// missing -> disclosed as missing (empty content, no withhold marker needed).
	missing := byID["finding:missing"]
	if missing[accessDispositionResponseKey] != accessDispositionMissing {
		t.Fatalf("missing disposition = %#v, want %q", missing[accessDispositionResponseKey], accessDispositionMissing)
	}
}

// TestDocumentationFindingsACLDistinctFromFreshness proves the ACL axis is
// independent of freshness (#2138): a fresh source can be denied, and a stale
// source can be allowed. Neither axis is derived from the other.
func TestDocumentationFindingsACLDistinctFromFreshness(t *testing.T) {
	t.Parallel()

	freshDenied := []byte(`{
		"finding_id": "finding:fresh-denied",
		"freshness_state": "fresh",
		"summary": "fresh content that is access-denied",
		"permissions": {"viewer_can_read_source": true},
		"acl_summary": {"source_acl_state": "denied"}
	}`)
	staleAllowed := []byte(`{
		"finding_id": "finding:stale-allowed",
		"freshness_state": "stale",
		"summary": "stale content the caller may read",
		"permissions": {"viewer_can_read_source": true},
		"acl_summary": {"source_acl_state": "allowed"}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{freshDenied}, {staleAllowed}},
	}})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{Limit: 50})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	byID := map[string]map[string]any{}
	for _, f := range got.Findings {
		byID[f["finding_id"].(string)] = f
	}

	fd := byID["finding:fresh-denied"]
	if fd["freshness_state"] != "fresh" {
		t.Fatalf("fresh+denied freshness_state = %#v, want 'fresh'", fd["freshness_state"])
	}
	if fd[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("fresh+denied disposition = %#v, want access_denied (independent of freshness)", fd[accessDispositionResponseKey])
	}

	sa := byID["finding:stale-allowed"]
	if sa["freshness_state"] != "stale" {
		t.Fatalf("stale+allowed freshness_state = %#v, want 'stale'", sa["freshness_state"])
	}
	if sa[accessDispositionResponseKey] != accessDispositionVisible {
		t.Fatalf("stale+allowed disposition = %#v, want visible (ACL allowed despite stale freshness)", sa[accessDispositionResponseKey])
	}
	if sa["summary"] != "stale content the caller may read" {
		t.Fatalf("stale+allowed row content withheld unexpectedly: %#v", sa)
	}
}

// TestDocumentationFindingsPreservesUnsupportedAndMissingEvidenceLabels proves
// the #2138 truth labels (missing_evidence, unsupported_reason) survive a
// content-withheld denied row and are not collapsed into the permission error.
func TestDocumentationFindingsPreservesUnsupportedAndMissingEvidenceLabels(t *testing.T) {
	t.Parallel()

	row := []byte(`{
		"finding_id": "finding:labels",
		"status": "unsupported",
		"unsupported_reason": "capability_not_enabled",
		"missing_evidence": ["deployment_trace"],
		"summary": "protected content",
		"permissions": {"viewer_can_read_source": true},
		"acl_summary": {"source_acl_state": "denied"}
	}`)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{row}},
	}})
	reader := NewContentReader(db)

	got, err := reader.documentationFindings(t.Context(), documentationFindingFilter{Limit: 50})
	if err != nil {
		t.Fatalf("documentationFindings() error = %v, want nil", err)
	}
	f := got.Findings[0]
	if f[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("disposition = %#v, want access_denied", f[accessDispositionResponseKey])
	}
	if f["unsupported_reason"] != "capability_not_enabled" {
		t.Fatalf("unsupported_reason collapsed/dropped: %#v", f)
	}
	if _, ok := f["missing_evidence"]; !ok {
		t.Fatalf("missing_evidence dropped on denied row: %#v", f)
	}
	if _, leaked := f["summary"]; leaked {
		t.Fatalf("denied row leaked content: %#v", f)
	}
}

// TestDocumentationEvidencePacketEnforcement proves the packet readback honors
// the matrix: denied -> Denied (handler returns permission_denied, body never
// returned), partial -> Available with content withheld behind a partial marker,
// stale -> Available with content intact and surfaced as stale.
func TestDocumentationEvidencePacketEnforcement(t *testing.T) {
	t.Parallel()

	packet := func(state string) []byte {
		return []byte(`{
			"packet_id": "packet:` + state + `",
			"finding_id": "finding:` + state + `",
			"permissions": {"viewer_can_read_source": true},
			"states": {"freshness_state": "fresh"},
			"bounded_excerpt": {"text": "PROTECTED ` + state + ` excerpt"},
			"acl_summary": {"source_acl_state": "` + state + `"}
		}`)
	}

	t.Run("denied", func(t *testing.T) {
		t.Parallel()
		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet(facts.SourceACLStateDenied)}},
		}})
		got, err := NewContentReader(db).documentationEvidencePacketWithFilter(
			t.Context(), documentationEvidencePacketFilter{FindingID: "finding:denied"},
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !got.Denied {
			t.Fatalf("denied packet must report Denied (permission_denied), got %#v", got)
		}
		if got.Available || len(got.Packet) != 0 {
			t.Fatalf("denied packet must not return a body: %#v", got)
		}
	})

	t.Run("partial withholds content", func(t *testing.T) {
		t.Parallel()
		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet(facts.SourceACLStatePartial)}},
		}})
		got, err := NewContentReader(db).documentationEvidencePacketWithFilter(
			t.Context(), documentationEvidencePacketFilter{FindingID: "finding:partial"},
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !got.Available || got.Denied {
			t.Fatalf("partial packet must be available and not denied: %#v", got)
		}
		if got.Packet[accessDispositionResponseKey] != accessDispositionPartial {
			t.Fatalf("partial disposition = %#v", got.Packet[accessDispositionResponseKey])
		}
		if got.Packet[contentWithheldResponseKey] != true {
			t.Fatalf("partial packet must withhold content: %#v", got.Packet)
		}
		if _, leaked := got.Packet["bounded_excerpt"]; leaked {
			t.Fatalf("partial packet leaked excerpt: %#v", got.Packet)
		}
	})

	t.Run("stale keeps content", func(t *testing.T) {
		t.Parallel()
		db := openContentReaderTestDB(t, []contentReaderQueryResult{{
			columns: []string{"payload"},
			rows:    [][]driver.Value{{packet(facts.SourceACLStateStale)}},
		}})
		got, err := NewContentReader(db).documentationEvidencePacketWithFilter(
			t.Context(), documentationEvidencePacketFilter{FindingID: "finding:stale"},
		)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !got.Available || got.Denied {
			t.Fatalf("stale packet must be available: %#v", got)
		}
		if got.Packet[accessDispositionResponseKey] != accessDispositionStale {
			t.Fatalf("stale disposition = %#v", got.Packet[accessDispositionResponseKey])
		}
		excerpt, ok := got.Packet["bounded_excerpt"].(map[string]any)
		if !ok || excerpt["text"] != "PROTECTED stale excerpt" {
			t.Fatalf("stale packet must keep readable content: %#v", got.Packet)
		}
	})
}

// TestSemanticEvidenceRowEnforcement proves semantic-evidence rows enforce the
// same matrix. A denied observation withholds its observation_text/hint_text and
// evidence_refs behind an access-denied disposition while preserving the #2138
// admission/freshness/corroboration labels; an allowed observation stays visible.
func TestSemanticEvidenceRowEnforcement(t *testing.T) {
	t.Parallel()

	deniedRaw := map[string]any{
		"fact_id":   "fact:denied",
		"fact_kind": facts.SemanticDocumentationObservationFactKind,
		"payload": map[string]any{
			"observation_type":    "deployment_claim",
			"observation_text":    "SECRET semantic observation text",
			"freshness_state":     facts.SemanticFreshnessFresh,
			"admission_state":     facts.SemanticAdmissionPartial,
			"corroboration_state": "uncorroborated",
			"evidence_refs":       []any{map[string]any{"kind": "doc", "id": "doc:1"}},
			"acl_summary":         map[string]any{"source_acl_state": facts.SourceACLStateDenied},
		},
	}
	deniedRow := semanticEvidencePublicRow(deniedRaw)
	if deniedRow[accessDispositionResponseKey] != accessDispositionDenied {
		t.Fatalf("denied semantic disposition = %#v", deniedRow[accessDispositionResponseKey])
	}
	if deniedRow[permissionDeniedResponseKey] != true || deniedRow[contentWithheldResponseKey] != true {
		t.Fatalf("denied semantic markers wrong: %#v", deniedRow)
	}
	if _, leaked := deniedRow["observation_text"]; leaked {
		t.Fatalf("denied semantic row leaked observation_text: %#v", deniedRow)
	}
	if _, leaked := deniedRow["evidence_refs"]; leaked {
		t.Fatalf("denied semantic row leaked evidence_refs: %#v", deniedRow)
	}
	// #2138: admission/freshness/corroboration preserved, not collapsed.
	if deniedRow["admission_state"] != facts.SemanticAdmissionPartial {
		t.Fatalf("admission_state collapsed on denied semantic row: %#v", deniedRow)
	}
	if deniedRow["freshness_state"] != facts.SemanticFreshnessFresh {
		t.Fatalf("freshness_state collapsed on denied semantic row: %#v", deniedRow)
	}
	if deniedRow["source_acl_state"] != facts.SourceACLStateDenied {
		t.Fatalf("denied semantic source_acl_state = %#v", deniedRow["source_acl_state"])
	}

	allowedRaw := map[string]any{
		"fact_id":   "fact:allowed",
		"fact_kind": facts.SemanticDocumentationObservationFactKind,
		"payload": map[string]any{
			"observation_type": "deployment_claim",
			"observation_text": "readable semantic observation",
			"acl_summary":      map[string]any{"source_acl_state": facts.SourceACLStateAllowed},
		},
	}
	allowedRow := semanticEvidencePublicRow(allowedRaw)
	if allowedRow[accessDispositionResponseKey] != accessDispositionVisible {
		t.Fatalf("allowed semantic disposition = %#v", allowedRow[accessDispositionResponseKey])
	}
	if allowedRow["observation_text"] != "readable semantic observation" {
		t.Fatalf("allowed semantic row dropped content: %#v", allowedRow)
	}
}
