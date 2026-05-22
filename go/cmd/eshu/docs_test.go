package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func TestDocsVerifyCommandIsRegistered(t *testing.T) {
	t.Parallel()

	cmd, _, err := rootCmd.Find([]string{"docs", "verify"})
	if err != nil {
		t.Fatalf("rootCmd.Find(docs verify) error = %v, want nil", err)
	}
	if cmd == nil {
		t.Fatal("docs verify command missing")
	}
}

func TestRunDocsVerifyJSONReportsContradictedClaims(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(docPath, []byte("Run `eshu vaporize all`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--json", "--fail-on", "contradicted"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want non-zero for contradicted finding")
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	data := envelope.Data
	if got, want := int(data.Summary.Contradicted), 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if got := envelope.Error; got == nil || !strings.Contains(got.Message, "contradicted") {
		t.Fatalf("Envelope.Error = %#v, want contradicted failure", got)
	}
}

func TestRunDocsVerifyTextReportsValidCommandAndEndpoint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "runbook.md")
	if err := os.WriteFile(
		docPath,
		[]byte("Run `eshu docs verify .` and call `GET /api/v0/documentation/findings`.\n"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	cmd := newTestDocsVerifyCommand(docsVerifyDeps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--limit", "5"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil; output=%s", err, out.String())
	}
	if !strings.Contains(out.String(), "valid=2") {
		t.Fatalf("output = %q, want valid=2", out.String())
	}
}

func TestReadDocumentInputBoundsContentButHashesFullFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "space name.md")
	fullContent := []byte("`eshu docs verify .`\nthis suffix must only affect revision\n")
	if err := os.WriteFile(docPath, fullContent, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}

	doc, err := readDocumentInput(docPath, 8)
	if err != nil {
		t.Fatalf("readDocumentInput() error = %v, want nil", err)
	}

	if got, want := len(doc.Content), 8; got != want {
		t.Fatalf("len(Content) = %d, want %d", got, want)
	}
	if !doc.ContentTruncated {
		t.Fatal("ContentTruncated = false, want true")
	}
	if got, want := doc.RevisionID, sha256Revision(fullContent); got != want {
		t.Fatalf("RevisionID = %q, want full-file hash %q", got, want)
	}
	if strings.Contains(doc.SourceURI, " ") {
		t.Fatalf("SourceURI = %q, want escaped file URI", doc.SourceURI)
	}
	if !strings.HasPrefix(doc.SourceURI, "file:///") {
		t.Fatalf("SourceURI = %q, want canonical file URI", doc.SourceURI)
	}
}

func TestInventoryDocsStopsAtLimitAndMarksTruncated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("`eshu docs verify .`\n"), 0o600); err != nil {
			t.Fatalf("WriteFile(%s) error = %v, want nil", name, err)
		}
	}

	inventory, err := inventoryDocs(docsVerifyOptions{Path: dir, Limit: 1, MaxDocumentBytes: 1024})
	if err != nil {
		t.Fatalf("inventoryDocs() error = %v, want nil", err)
	}
	if got, want := len(inventory.Documents), 1; got != want {
		t.Fatalf("len(Documents) = %d, want %d", got, want)
	}
	if !inventory.Truncated {
		t.Fatal("Truncated = false, want true when the file limit stops traversal")
	}
}

func TestDocsVerifyEnvironmentTruthUsesReferenceDocs(t *testing.T) {
	t.Parallel()

	vars := map[string]struct{}{}
	for _, name := range docsVerifyEnvironmentTruth(".") {
		vars[name] = struct{}{}
	}
	for _, name := range []string{"ESHU_HOME", "ESHU_QUERY_PROFILE", "ESHU_FACT_STORE_DSN"} {
		if _, ok := vars[name]; !ok {
			t.Fatalf("docsVerifyEnvironmentTruth() missing %s from reference docs", name)
		}
	}
}

func TestRunDocsVerifyPersistCommitsDocumentationFacts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(docPath, []byte("Run `eshu docs verify .`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	persistence := &recordingDocsVerifyPersistence{}
	cmd := newTestDocsVerifyCommand(docsVerifyDeps{
		openPersistence: fixedDocsPersistence(persistence),
		now:             fixedDocsNow,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--persist", "--json"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil; output=%s", err, out.String())
	}

	if got, want := len(persistence.commits), 1; got != want {
		t.Fatalf("commit count = %d, want %d", got, want)
	}
	commit := persistence.commits[0]
	if got, want := commit.scopeValue.ScopeKind, scope.KindDocumentationSource; got != want {
		t.Fatalf("scope kind = %q, want %q", got, want)
	}
	if got, want := commit.scopeValue.CollectorKind, scope.CollectorDocumentation; got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
	assertCommittedFactKinds(t, commit.envelopes, facts.DocumentationFindingFactKind, facts.DocumentationEvidencePacketFactKind)
	for _, envelope := range commit.envelopes {
		if envelope.ScopeID != commit.scopeValue.ScopeID {
			t.Fatalf("envelope scope = %q, want %q", envelope.ScopeID, commit.scopeValue.ScopeID)
		}
		if envelope.GenerationID != commit.generation.GenerationID {
			t.Fatalf("envelope generation = %q, want %q", envelope.GenerationID, commit.generation.GenerationID)
		}
	}

	var envelope docsVerifyEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if !envelope.Data.Persistence.Persisted || envelope.Data.Persistence.Skipped {
		t.Fatalf("Persistence = %#v, want persisted true and skipped false", envelope.Data.Persistence)
	}
	if envelope.Data.Persistence.ScopeID == "" || envelope.Data.Persistence.GenerationID == "" {
		t.Fatalf("Persistence = %#v, want scope and generation ids", envelope.Data.Persistence)
	}
}

func TestRunDocsVerifyPersistSkipsUnchangedAndReturnsStoredFindings(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(docPath, []byte("Run `eshu vaporize all`.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	inventory, err := inventoryDocs(docsVerifyOptions{Path: docPath, Limit: 50, MaxDocumentBytes: 256 * 1024})
	if err != nil {
		t.Fatalf("inventoryDocs() error = %v, want nil", err)
	}
	scopeID := docsVerifyScopeID(docPath, "")
	generationID := "docs-verify-generation-existing"
	persistence := &recordingDocsVerifyPersistence{
		current: docsPersistedGeneration{
			GenerationID:  generationID,
			FreshnessHint: docsInventoryFreshnessHint(inventory.Documents, 256*1024, 50, "local"),
		},
		currentFound: true,
		listed: []facts.Envelope{
			storedDocumentationFinding(scopeID, generationID, "contradicted"),
			storedDocumentationPacket(scopeID, generationID),
		},
	}
	cmd := newTestDocsVerifyCommand(docsVerifyDeps{
		openPersistence: fixedDocsPersistence(persistence),
		now:             fixedDocsNow,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--persist", "--json", "--fail-on", "contradicted"})
	err = cmd.Execute()
	if err == nil {
		t.Fatal("docs verify error = nil, want stored contradicted finding to fail")
	}
	if got := len(persistence.commits); got != 0 {
		t.Fatalf("commit count = %d, want 0 for unchanged persisted docs", got)
	}

	var envelope docsVerifyEnvelope
	if decodeErr := json.Unmarshal(out.Bytes(), &envelope); decodeErr != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", decodeErr, out.String())
	}
	if got, want := envelope.Data.Summary.Contradicted, 1; got != want {
		t.Fatalf("Summary.Contradicted = %d, want %d", got, want)
	}
	if !envelope.Data.Persistence.Skipped || envelope.Data.Persistence.Persisted {
		t.Fatalf("Persistence = %#v, want skipped true and persisted false", envelope.Data.Persistence)
	}
	if got := envelope.Error; got == nil || !strings.Contains(got.Message, "contradicted") {
		t.Fatalf("Envelope.Error = %#v, want contradicted failure", got)
	}
}

func TestRunDocsVerifyPersistDoesNotSkipWhenMaxBytesChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(docPath, []byte("Run `eshu docs verify .`.\nThis suffix keeps the file truncated.\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	previousInventory, err := inventoryDocs(docsVerifyOptions{Path: docPath, Limit: 50, MaxDocumentBytes: 8})
	if err != nil {
		t.Fatalf("inventoryDocs() error = %v, want nil", err)
	}
	scopeID := docsVerifyScopeID(docPath, "")
	persistence := &recordingDocsVerifyPersistence{
		current: docsPersistedGeneration{
			GenerationID:  "docs-verify-generation-existing",
			FreshnessHint: docsInventoryFreshnessHint(previousInventory.Documents, 8, 50, "local"),
		},
		currentFound: true,
	}
	cmd := newTestDocsVerifyCommand(docsVerifyDeps{
		openPersistence: fixedDocsPersistence(persistence),
		now:             fixedDocsNow,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--persist", "--json", "--max-bytes", "16"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil; output=%s", err, out.String())
	}

	if got, want := len(persistence.commits), 1; got != want {
		t.Fatalf("commit count = %d, want %d when max-bytes changes for truncated docs", got, want)
	}
	if got := persistence.commits[0].scopeValue.ScopeID; got != scopeID {
		t.Fatalf("committed scope = %q, want %q", got, scopeID)
	}
	var envelope docsVerifyEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if envelope.Data.Persistence.Skipped {
		t.Fatalf("Persistence = %#v, want skipped false after max-bytes changes", envelope.Data.Persistence)
	}
}

func TestRunDocsVerifyPersistSkipReportsCurrentInventoryCountersAndTruncation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	docPath := filepath.Join(dir, "README.md")
	content := []byte("Run `eshu docs verify .`.\nThis suffix is outside the bounded scan.\n")
	if err := os.WriteFile(docPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v, want nil", err)
	}
	opts := docsVerifyOptions{Path: docPath, Limit: 50, MaxDocumentBytes: 12}
	inventory, err := inventoryDocs(opts)
	if err != nil {
		t.Fatalf("inventoryDocs() error = %v, want nil", err)
	}
	scopeID := docsVerifyScopeID(docPath, "")
	generationID := "docs-verify-generation-existing"
	persistence := &recordingDocsVerifyPersistence{
		current: docsPersistedGeneration{
			GenerationID:  generationID,
			FreshnessHint: docsInventoryFreshnessHint(inventory.Documents, opts.MaxDocumentBytes, opts.Limit, "local"),
		},
		currentFound: true,
		listed: []facts.Envelope{
			storedDocumentationFinding(scopeID, generationID, "valid"),
			storedDocumentationPacket(scopeID, generationID),
		},
	}
	cmd := newTestDocsVerifyCommand(docsVerifyDeps{
		openPersistence: fixedDocsPersistence(persistence),
		now:             fixedDocsNow,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{docPath, "--persist", "--json", "--max-bytes", "12"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("docs verify error = %v, want nil; output=%s", err, out.String())
	}

	var envelope docsVerifyEnvelope
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil; output=%s", err, out.String())
	}
	if got, want := envelope.Data.Summary.DocumentsScanned, 1; got != want {
		t.Fatalf("DocumentsScanned = %d, want %d", got, want)
	}
	if got, want := envelope.Data.Summary.BytesScanned, 12; got != want {
		t.Fatalf("BytesScanned = %d, want %d", got, want)
	}
	if !envelope.Data.Truncated {
		t.Fatal("Truncated = false, want true for byte-truncated persisted replay")
	}
	if !envelope.Data.Persistence.Skipped {
		t.Fatalf("Persistence = %#v, want skipped true", envelope.Data.Persistence)
	}
}

func newTestDocsVerifyCommand(deps docsVerifyDeps) *cobra.Command {
	if deps.commandTruth == nil {
		deps.commandTruth = fixedDocsCommandTruth
	}
	return newDocsVerifyCommandWithDeps(deps)
}

func fixedDocsCommandTruth() []doctruth.CommandTruth {
	return []doctruth.CommandTruth{
		{Path: []string{"docs", "verify"}},
		{Path: []string{"scan"}},
		{Path: []string{"trace", "service"}},
		{Path: []string{"map"}},
	}
}

func sha256Revision(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type docsVerifyCommit struct {
	scopeValue scope.IngestionScope
	generation scope.ScopeGeneration
	envelopes  []facts.Envelope
}

type recordingDocsVerifyPersistence struct {
	current      docsPersistedGeneration
	currentFound bool
	listed       []facts.Envelope
	commits      []docsVerifyCommit
}

func (p *recordingDocsVerifyPersistence) CurrentGeneration(
	context.Context,
	string,
) (docsPersistedGeneration, bool, error) {
	return p.current, p.currentFound, nil
}

func (p *recordingDocsVerifyPersistence) ListFactEnvelopes(
	context.Context,
	string,
	string,
	[]string,
) ([]facts.Envelope, error) {
	return p.listed, nil
}

func (p *recordingDocsVerifyPersistence) CommitScopeGeneration(
	_ context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
) error {
	commit := docsVerifyCommit{scopeValue: scopeValue, generation: generation}
	for envelope := range factStream {
		commit.envelopes = append(commit.envelopes, envelope)
	}
	p.commits = append(p.commits, commit)
	return nil
}

func fixedDocsPersistence(p docsVerifyPersistence) docsPersistenceFactory {
	return func(context.Context) (docsVerifyPersistence, func() error, error) {
		return p, func() error { return nil }, nil
	}
}

func fixedDocsNow() time.Time {
	return time.Date(2026, time.May, 20, 18, 30, 0, 0, time.UTC)
}

func assertCommittedFactKinds(t *testing.T, envelopes []facts.Envelope, wantKinds ...string) {
	t.Helper()

	seen := map[string]struct{}{}
	for _, envelope := range envelopes {
		seen[envelope.FactKind] = struct{}{}
	}
	for _, kind := range wantKinds {
		if _, ok := seen[kind]; !ok {
			t.Fatalf("committed fact kinds = %#v, missing %s", seen, kind)
		}
	}
}

func storedDocumentationFinding(scopeID, generationID, status string) facts.Envelope {
	return facts.Envelope{
		FactID:           "finding-fact-1",
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.DocumentationFindingFactKind,
		StableFactKey:    "finding-stable-1",
		SchemaVersion:    facts.DocumentationFactSchemaVersion,
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceDerived,
		ObservedAt:       fixedDocsNow(),
		Payload: map[string]any{
			"finding_id":         "finding:stored",
			"finding_version":    "v1",
			"finding_type":       "documentation_claim_verification",
			"status":             status,
			"truth_level":        "derived",
			"freshness_state":    "fresh",
			"source_id":          "doc-source:stored",
			"document_id":        "doc:stored",
			"section_id":         "line:stored",
			"claim_id":           "claim:stored",
			"claim_type":         "cli_command",
			"claim_text":         "eshu vaporize all",
			"normalized_claim":   "vaporize all",
			"summary":            "stored contradicted claim",
			"evidence_packet_id": "doc-packet:stored",
		},
	}
}

func storedDocumentationPacket(scopeID, generationID string) facts.Envelope {
	return facts.Envelope{
		FactID:           "packet-fact-1",
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         facts.DocumentationEvidencePacketFactKind,
		StableFactKey:    "packet-stable-1",
		SchemaVersion:    facts.DocumentationFactSchemaVersion,
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceDerived,
		ObservedAt:       fixedDocsNow(),
		Payload: map[string]any{
			"packet_id":      "doc-packet:stored",
			"packet_version": "v1",
			"finding_id":     "finding:stored",
		},
	}
}
