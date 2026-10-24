// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8812 disposable PostgreSQL proof that a database already holding a verbatim content_files row, a content_file_references row, a content_file_secret_lines row, and a content_entities row with a non-empty source_cache holds an empty or marker-redacted body for that path after the next projected generation under a metadata or redact content-store policy, on both a full-snapshot and a delta generation, through the production projector runtime and content writer and migration 138 triggers; isolated bootstrapped database per test
//eshu:readiness:env ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	gitcontent "github.com/eshu-hq/eshu/go/internal/collector/git/content"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/projector/runtime"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	policyUpgradeSeed = "s3cr3tUpgradeSeed0123456789"
	policyUpgradePath = "config/.npmrc"
	policyUpgradeBody = "registry=https://registry.example.com/\n//registry.example.com/:_authToken=" + policyUpgradeSeed + "\n"
)

// policyUpgradeIntents accepts the reducer intents the projection emits; this
// proof reads the content store, not the reducer queue.
type policyUpgradeIntents struct{}

func (policyUpgradeIntents) Enqueue(_ context.Context, intents []runtime.ReducerIntent) (runtime.IntentResult, error) {
	return runtime.IntentResult{Count: len(intents)}, nil
}

type policyUpgradeProof struct {
	t        *testing.T
	ctx      context.Context
	db       *sql.DB
	repo     repositoryidentity.Metadata
	scope    scope.IngestionScope
	runtime  runtime.Runtime
	observed time.Time
}

func newPolicyUpgradeProof(t *testing.T) *policyUpgradeProof {
	t.Helper()
	// The isolated, bootstrapped proof schema the readiness runner provides
	// (ESHU_PROJECTOR_SUPERSESSION_PROOF_DSN, a disposable database).
	ctx, db := context.Background(), openReprojectProof(t).db
	repo, err := repositoryidentity.MetadataFor("proof-8812", "/repos/proof-8812", "https://github.com/eshu-proof/proof-8812.git")
	if err != nil {
		t.Fatalf("repository identity: %v", err)
	}
	return &policyUpgradeProof{
		t: t, ctx: ctx, db: db, repo: repo,
		scope: scope.IngestionScope{
			ScopeID: "git-repository-scope:" + repo.ID, SourceSystem: "git", ScopeKind: scope.KindRepository,
			CollectorKind: scope.CollectorGit, PartitionKey: repo.ID,
			Metadata: map[string]string{
				"repo_id": repo.ID, "repo_name": repo.Name, "source_key": repo.ID,
				"remote_url": repo.RemoteURL, "local_path": repo.LocalPath,
			},
		},
		runtime: runtime.Runtime{
			CanonicalWriter: &recordingCanonicalWriter{},
			ContentWriter:   NewContentWriter(SQLDB{DB: db}),
			IntentWriter:    policyUpgradeIntents{},
		},
		observed: time.Now().UTC().Add(-time.Hour).Truncate(time.Second),
	}
}

// project builds the facts the git collector emits for one generation of the
// single proof file and runs them through the production projector runtime.
// storedBody and entityCache are what the collector write boundary stored for
// the path; digest is always the SOURCE digest.
func (p *policyUpgradeProof) project(generationID string, delta bool, storedBody, entityCache string) {
	p.t.Helper()
	p.observed = p.observed.Add(time.Minute)
	scopeID := p.scope.ScopeID
	repoPath := p.repo.LocalPath
	generation := scope.ScopeGeneration{
		GenerationID: generationID, ScopeID: scopeID, ObservedAt: p.observed, IngestedAt: p.observed,
		Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot,
		FreshnessHint: "hint-" + generationID, SourceCommitSHA: "commit-" + generationID, IsDelta: delta,
	}
	if delta {
		generation.DeltaBaselineCommitSHA = "commit-gen1"
	}
	var changed []string
	if delta {
		changed = []string{policyUpgradePath}
	}
	envelopes := []facts.Envelope{
		gitcontent.RepositoryFactEnvelope(repoPath, p.repo, "run-"+generationID, scopeID, generationID, p.observed, 1,
			nil, false, "main", nil, delta, changed, nil, false, false),
		gitcontent.FileFactEnvelope(repoPath, p.repo.ID, scopeID, generationID, p.observed, map[string]any{
			"path": repoPath + "/" + policyUpgradePath, "language": "raw_text",
		}, false),
		gitcontent.ContentFactEnvelope(repoPath, p.repo.ID, scopeID, generationID, p.observed, model.ContentFileSnapshot{
			RelativePath: policyUpgradePath, Body: storedBody, Digest: "source-digest-8812", Language: "raw_text",
			CommitSHA: "commit-" + generationID,
		}),
		gitcontent.ContentEntityFactEnvelope(repoPath, p.repo.ID, scopeID, generationID, p.observed, model.ContentEntitySnapshot{
			EntityID: "content-entity:proof-8812:npmrc", RelativePath: policyUpgradePath, EntityType: "Variable",
			EntityName: "npmrc", StartLine: 1, EndLine: 2, Language: "raw_text", SourceCache: entityCache, IndexedAt: p.observed,
		}),
	}
	if _, err := p.runtime.Project(p.ctx, p.scope, generation, envelopes); err != nil {
		p.t.Fatalf("project %s: %v", generationID, err)
	}
}

type policyUpgradeState struct {
	content, contentLower, contentHash, sourceCache string
	lineCount, secretLines, references, entities    int
	seedInSecretLines                               int
}

func (p *policyUpgradeProof) state() policyUpgradeState {
	p.t.Helper()
	var s policyUpgradeState
	if err := p.db.QueryRowContext(p.ctx,
		`SELECT content, content_lower, content_hash, line_count FROM content_files WHERE repo_id = $1 AND relative_path = $2`,
		p.repo.ID, policyUpgradePath).Scan(&s.content, &s.contentLower, &s.contentHash, &s.lineCount); err != nil {
		p.t.Fatalf("read content_files row: %v", err)
	}
	counts := []struct {
		dst   *int
		query string
	}{
		{&s.secretLines, `SELECT count(*) FROM content_file_secret_lines WHERE repo_id = $1 AND relative_path = $2`},
		{&s.references, `SELECT count(*) FROM content_file_references WHERE repo_id = $1 AND relative_path = $2`},
		{&s.entities, `SELECT count(*) FROM content_entities WHERE repo_id = $1 AND relative_path = $2`},
		{&s.seedInSecretLines, `SELECT count(*) FROM content_file_secret_lines WHERE repo_id = $1 AND relative_path = $2 AND strpos(line_text, '` + policyUpgradeSeed + `') > 0`},
	}
	for _, c := range counts {
		if err := p.db.QueryRowContext(p.ctx, c.query, p.repo.ID, policyUpgradePath).Scan(c.dst); err != nil {
			p.t.Fatalf("count %.60q: %v", c.query, err)
		}
	}
	if err := p.db.QueryRowContext(p.ctx,
		`SELECT coalesce(source_cache, '') FROM content_entities WHERE repo_id = $1 AND relative_path = $2`,
		p.repo.ID, policyUpgradePath).Scan(&s.sourceCache); err != nil {
		p.t.Fatalf("read source_cache: %v", err)
	}
	return s
}

// TestContentStorePolicyHealsStoredRowsOnTheNextGeneration is the #8812 upgrade
// test. It seeds a verbatim row set first (content, content_lower, a derived
// secret line, a derived reference, an entity source_cache), then projects the
// next generation with the body the collector now stores, and checks the four
// copies a credential could live in. The content record is still emitted, so
// the upsert overwrites on a full snapshot and on a delta alike; there is no
// tombstone and no RetainedPaths change.
func TestContentStorePolicyHealsStoredRowsOnTheNextGeneration(t *testing.T) {
	cases := []struct {
		name  string
		store parser.ContentStore
		check func(t *testing.T, s policyUpgradeState)
	}{
		{
			name:  "metadata",
			store: parser.ContentStore{Policy: parser.ContentStoreMetadata},
			check: func(t *testing.T, s policyUpgradeState) {
				if s.content != "" || s.contentLower != "" || s.lineCount != 0 {
					t.Errorf("metadata row kept a body: content %q lower %q line_count %d", s.content, s.contentLower, s.lineCount)
				}
				if s.secretLines != 0 || s.references != 0 {
					t.Errorf("metadata row kept derived rows: secret lines %d, references %d", s.secretLines, s.references)
				}
				if s.sourceCache != "" {
					t.Errorf("metadata source_cache = %q, want empty", s.sourceCache)
				}
			},
		},
		{
			name:  "redact",
			store: parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: parser.RedactorMarker, CredentialKeys: []string{"_authToken"}},
			check: func(t *testing.T, s policyUpgradeState) {
				const want = "registry=https://registry.example.com/\n//registry.example.com/:_authToken=REDACTED_api_token\n"
				if s.content != want || s.contentLower != strings.ToLower(want) {
					t.Errorf("redact body = %q (lower %q), want %q", s.content, s.contentLower, want)
				}
				if s.sourceCache != want {
					t.Errorf("redact source_cache = %q, want it sliced from the redacted body", s.sourceCache)
				}
				if s.secretLines != 1 || s.seedInSecretLines != 0 {
					t.Errorf("redact secret lines = %d (seed in %d), want the marker line only", s.secretLines, s.seedInSecretLines)
				}
			},
		},
	}
	for _, tc := range cases {
		for _, delta := range []bool{false, true} {
			name := tc.name + "/full_snapshot"
			if delta {
				name = tc.name + "/delta"
			}
			t.Run(name, func(t *testing.T) {
				proof := newPolicyUpgradeProof(t)

				// Seed: a verbatim generation under the old (store) policy.
				proof.project("gen1", false, policyUpgradeBody, policyUpgradeBody)
				seeded := proof.state()
				if !strings.Contains(seeded.content, policyUpgradeSeed) || !strings.Contains(seeded.contentLower, strings.ToLower(policyUpgradeSeed)) ||
					!strings.Contains(seeded.sourceCache, policyUpgradeSeed) || seeded.secretLines == 0 || seeded.seedInSecretLines == 0 || seeded.references == 0 {
					t.Fatalf("seed is not a verbatim row set (the test would pass on a fresh database): %+v", seeded)
				}

				// The next generation, under the new policy, applied at the
				// collector write boundary exactly as streamFacts applies it.
				stored := gitcontent.ApplyContentStore(tc.store, policyUpgradeBody)
				proof.project("gen2", delta, stored.Body, stored.Body)

				after := proof.state()
				if after.contentHash != "source-digest-8812" {
					t.Errorf("content_hash = %q, want the source digest", after.contentHash)
				}
				if after.entities != 1 {
					t.Errorf("entity rows = %d, want the entity kept", after.entities)
				}
				for label, text := range map[string]string{
					"content": after.content, "content_lower": after.contentLower, "source_cache": after.sourceCache,
				} {
					if strings.Contains(strings.ToLower(text), strings.ToLower(policyUpgradeSeed)) {
						t.Errorf("seeded credential still in %s: %q", label, text)
					}
				}
				if after.seedInSecretLines != 0 {
					t.Errorf("seeded credential still in content_file_secret_lines.line_text")
				}
				tc.check(t, after)
			})
		}
	}
}
