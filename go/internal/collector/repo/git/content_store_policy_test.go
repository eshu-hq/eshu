// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const (
	policySeed       = "s3cr3tPolicySeed0123456789"
	policyNpmrcBody  = "registry=https://registry.example.com/\n//registry.example.com/:_authToken=" + policySeed + "\nalways-auth=true\n"
	policyYarnBody   = "nodeLinker: node-modules\nnpmRegistryServer: \"https://registry.example.com/\"\nnpmAuthToken: " + policySeed + "\n"
	policyTfBody     = "variable \"api_secret\" {\n  description = \"the key\"\n  default     = \"" + policySeed + "\"\n}\n"
	policyObservedAt = "2026-10-24T09:00:00Z"
)

// policyRegistry returns the default registry with one definition's content
// store replaced and, when extraExact is non-empty, extra exact names bound to
// it. The default registry binds no non-store policy before #8833, so every
// policy test builds its own.
func policyRegistry(t *testing.T, parserKey string, store parser.ContentStore, extraExact ...string) parser.Registry {
	t.Helper()
	definitions := parser.DefaultRegistry().Definitions()
	found := false
	for i := range definitions {
		if definitions[i].ParserKey != parserKey {
			continue
		}
		found = true
		definitions[i].ContentStore = store
		definitions[i].ExactNames = append(definitions[i].ExactNames, extraExact...)
	}
	if !found {
		t.Fatalf("no parser definition %q", parserKey)
	}
	registry, err := parser.NewRegistry(definitions)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func policyMarker(keys ...string) parser.ContentStore {
	return parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: parser.RedactorMarker, CredentialKeys: keys}
}

// snapshotWithPolicy snapshots a one-file repository under a registry and
// returns the snapshot plus every fact the stream emits for it.
func snapshotWithPolicy(t *testing.T, registry parser.Registry, relativePath, body string) (RepositorySnapshot, []facts.Envelope) {
	t.Helper()
	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, ".git", "HEAD"), "ref: refs/heads/main\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, filepath.FromSlash(relativePath)), body)
	resolved, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		resolved = repoRoot
	}
	engine, err := parser.NewEngine(registry, parser.NewRuntime())
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	snapshot, err := (NativeRepositorySnapshotter{Engine: engine, Registry: registry}).SnapshotRepository(
		context.Background(), SelectedRepository{RepoPath: resolved})
	if err != nil {
		t.Fatalf("SnapshotRepository: %v", err)
	}
	observedAt, _ := time.Parse(time.RFC3339, policyObservedAt)
	// streamFacts zeroes each meta as it emits it, so keep a copy for the
	// digest and freshness-hint assertions.
	metas := slices.Clone(snapshot.ContentFileMetas)
	collected := buildStreamingGeneration(resolved, repositoryidentity.Metadata{ID: "repo://example/policy", Name: "policy"},
		"run", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)
	snapshot.ContentFileMetas = metas
	return snapshot, envelopes
}

func contentFact(t *testing.T, envelopes []facts.Envelope, path string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == "content" && envelope.Payload["content_path"] == path {
			return envelope
		}
	}
	t.Fatalf("no content fact for %q among %d facts", path, len(envelopes))
	return facts.Envelope{}
}

// assertNoSeedInAnyFact fails if the seeded credential reaches any fact payload.
func assertNoSeedInAnyFact(t *testing.T, envelopes []facts.Envelope, seed string) {
	t.Helper()
	for _, envelope := range envelopes {
		raw, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatalf("marshal %s payload: %v", envelope.FactKind, err)
		}
		if strings.Contains(string(raw), seed) {
			t.Fatalf("seeded credential reached a %s fact payload: %s", envelope.FactKind, raw)
		}
	}
}

// assertNoSeedInContentFacts checks the facts that carry file text: content,
// content_entity source_cache, and the whole-file parsed_file_data.source.
// Parser-extracted structured values (a Terraform variable default in
// entity_metadata or parsed_file_data) are parser output, not a copy of the
// body, so the policy does not govern them; #8833 decides per binding.
func assertNoSeedInContentFacts(t *testing.T, envelopes []facts.Envelope, seed string) {
	t.Helper()
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case "content":
			assertNoSeedInAnyFact(t, []facts.Envelope{envelope}, seed)
		case "content_entity":
			if strings.Contains(asString(envelope.Payload["source_cache"]), seed) {
				t.Fatalf("seeded credential reached content_entity source_cache: %q", envelope.Payload["source_cache"])
			}
		case "file":
			data, _ := envelope.Payload["parsed_file_data"].(map[string]any)
			if strings.Contains(asString(data["source"]), seed) {
				t.Fatalf("seeded credential reached parsed_file_data.source: %q", data["source"])
			}
		}
	}
}

func TestMetadataPolicyEmitsAnEmptyBodyWithTheSourceDigest(t *testing.T) {
	t.Parallel()

	registry := policyRegistry(t, "raw_text", parser.ContentStore{Policy: parser.ContentStoreMetadata}, ".npmrc")
	snapshot, envelopes := snapshotWithPolicy(t, registry, ".npmrc", policyNpmrcBody)

	fact := contentFact(t, envelopes, ".npmrc")
	if fact.Payload["content_body"] != "" {
		t.Fatalf("metadata content_body = %q, want empty", fact.Payload["content_body"])
	}
	if want := digestForBody(policyNpmrcBody); fact.Payload["content_digest"] != want {
		t.Fatalf("content_digest = %v, want the SOURCE digest %s", fact.Payload["content_digest"], want)
	}
	assertNoSeedInContentFacts(t, envelopes, policySeed)
	assertNoSeedInAnyFact(t, envelopes, policySeed)
	if len(snapshot.ContentFileMetas) == 0 || snapshot.ContentFileMetas[0].Digest != digestForBody(policyNpmrcBody) {
		t.Fatalf("snapshot meta digest must be the source digest: %+v", snapshot.ContentFileMetas)
	}
	var fileFact bool
	for _, envelope := range envelopes {
		if envelope.FactKind == "file" && strings.HasSuffix(asString(envelope.Payload["relative_path"]), ".npmrc") {
			fileFact = true
		}
	}
	if !fileFact {
		t.Fatal("the parser still emits the file fact for a metadata path")
	}
}

func TestRedactPolicyEmitsTheMarkedBodyWithTheSourceDigest(t *testing.T) {
	t.Parallel()

	registry := policyRegistry(t, "yaml", policyMarker("npmAuthToken", "npmAuthIdent"), ".yarnrc.yml")
	_, envelopes := snapshotWithPolicy(t, registry, ".yarnrc.yml", policyYarnBody)

	fact := contentFact(t, envelopes, ".yarnrc.yml")
	want := "nodeLinker: node-modules\nnpmRegistryServer: \"https://registry.example.com/\"\nnpmAuthToken: REDACTED_api_token\n"
	if fact.Payload["content_body"] != want {
		t.Fatalf("content_body = %q, want %q", fact.Payload["content_body"], want)
	}
	if want := digestForBody(policyYarnBody); fact.Payload["content_digest"] != want {
		t.Fatalf("content_digest = %v, want the SOURCE digest %s", fact.Payload["content_digest"], want)
	}
	assertNoSeedInAnyFact(t, envelopes, policySeed)
}

func TestStorePolicyIsUnchangedByDefault(t *testing.T) {
	t.Parallel()

	_, envelopes := snapshotWithPolicy(t, parser.DefaultRegistry(), "app.yaml", policyYarnBody)
	fact := contentFact(t, envelopes, "app.yaml")
	if fact.Payload["content_body"] != policyYarnBody {
		t.Fatalf("default registry must store the body verbatim, got %q", fact.Payload["content_body"])
	}
}

// TestFreshnessHintIsStableAcrossCyclesAndPolicies pins the digest-before-policy
// rule: an unchanged file keeps one freshness hint across two cycles, and the
// hint does not depend on the policy, because the digest is the source digest.
// The registry fingerprint, not the digest, is what invalidates on a policy change.
func TestFreshnessHintIsStableAcrossCyclesAndPolicies(t *testing.T) {
	t.Parallel()

	const version = "git-collector.test"
	policies := map[string]parser.ContentStore{
		"store":    {},
		"metadata": {Policy: parser.ContentStoreMetadata},
		"redact":   policyMarker("npmAuthToken"),
	}
	hints := map[string]string{}
	for name, store := range policies {
		registry := policyRegistry(t, "yaml", store, ".yarnrc.yml")
		first, _ := snapshotWithPolicy(t, registry, ".yarnrc.yml", policyYarnBody)
		second, _ := snapshotWithPolicy(t, registry, ".yarnrc.yml", policyYarnBody)
		a := snapshotFreshnessHintForVersion(first, version)
		b := snapshotFreshnessHintForVersion(second, version)
		if a != b {
			t.Errorf("%s: freshness hint moved between two cycles of an unchanged file", name)
		}
		hints[name] = a
	}
	if hints["store"] != hints["metadata"] || hints["store"] != hints["redact"] {
		t.Errorf("freshness hint depends on the policy, so the digest was not computed on the source bytes: %v", hints)
	}
}

func TestPolicyFingerprintMovesWhenDefinitionPolicyChanges(t *testing.T) {
	t.Parallel()

	store := policyRegistry(t, "yaml", parser.ContentStore{}, ".yarnrc.yml").Fingerprint()
	redact := policyRegistry(t, "yaml", policyMarker("npmAuthToken"), ".yarnrc.yml").Fingerprint()
	if store == redact {
		t.Fatal("a policy change must move the registry fingerprint so every repository re-collects once")
	}
}

// TestEntitySourceCacheFollowsThePolicy proves source_cache is sliced from the
// stored body, never the source: under redact the slice carries the marker,
// under metadata it is empty, and the control run shows the seed would
// otherwise reach source_cache.
func TestEntitySourceCacheFollowsThePolicy(t *testing.T) {
	t.Parallel()

	entityCaches := func(envelopes []facts.Envelope) []string {
		var caches []string
		for _, envelope := range envelopes {
			if envelope.FactKind == "content_entity" && envelope.Payload["relative_path"] == "vars.tf" {
				caches = append(caches, asString(envelope.Payload["source_cache"]))
			}
		}
		return caches
	}

	_, control := snapshotWithPolicy(t, parser.DefaultRegistry(), "vars.tf", policyTfBody)
	var controlLeaks bool
	for _, cache := range entityCaches(control) {
		controlLeaks = controlLeaks || strings.Contains(cache, policySeed)
	}
	if !controlLeaks {
		t.Fatalf("control: the seed must reach source_cache under the store policy, got %q", entityCaches(control))
	}

	_, redacted := snapshotWithPolicy(t, policyRegistry(t, "hcl", policyMarker("default")), "vars.tf", policyTfBody)
	caches := entityCaches(redacted)
	if len(caches) == 0 {
		t.Fatal("redact policy must keep the content entities")
	}
	var marked bool
	for _, cache := range caches {
		marked = marked || strings.Contains(cache, "REDACTED_")
	}
	if !marked {
		t.Fatalf("redact source_cache must be sliced from the redacted body, got %q", caches)
	}
	assertNoSeedInContentFacts(t, redacted, policySeed)

	_, metadata := snapshotWithPolicy(t, policyRegistry(t, "hcl", parser.ContentStore{Policy: parser.ContentStoreMetadata}), "vars.tf", policyTfBody)
	metaCaches := entityCaches(metadata)
	if len(metaCaches) == 0 {
		t.Fatal("metadata policy must keep the content entities")
	}
	for _, cache := range metaCaches {
		if cache != "" {
			t.Fatalf("metadata source_cache must be empty, got %q", cache)
		}
	}
	assertNoSeedInContentFacts(t, metadata, policySeed)
}

func TestRedactFailureStoresAnEmptyBodyNeverTheSource(t *testing.T) {
	t.Parallel()

	// raw_text accepts the invalid byte; the yaml parser would skip the file.
	registry := policyRegistry(t, "raw_text", policyMarker("_authToken"), ".npmrc")
	body := "//registry.example.com/:_authToken=" + policySeed + "\xff\n"
	_, envelopes := snapshotWithPolicy(t, registry, ".npmrc", body)
	fact := contentFact(t, envelopes, ".npmrc")
	if fact.Payload["content_body"] != "" {
		t.Fatalf("a non-UTF-8 redact path must store an empty body, got %q", fact.Payload["content_body"])
	}
	if fact.Payload["content_digest"] != digestForBody(body) {
		t.Fatal("the metadata fallback keeps the source digest")
	}
	assertNoSeedInAnyFact(t, envelopes, policySeed)
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
