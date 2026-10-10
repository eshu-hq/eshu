// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

const hydrationTestRepoID = "repo-anchor"

// hydrationStore is an in-memory content store for the needed-content
// hydration tests. It lists metadata-only rows, answers GetFileContent and
// NonBlankRepoFilePaths from the same rows, and records every statement so a
// test can assert what moved.
type hydrationStore struct {
	querycontract.ContentStore

	mu    sync.Mutex
	rows  map[string]querycontract.FileContent
	gone  map[string]bool
	getAt map[string]error
	// goneAfterCheck rows answer non-blank to NonBlankRepoFilePaths and then
	// vanish: GetFileContent returns no row, as when a file is deleted between
	// the two statements.
	goneAfterCheck map[string]bool

	blankErr error
	// blankMode replaces the non-blank answer, to mutate the SQL predicate.
	blankMode func(content string) bool

	// getCalls lists every path whose content was read, by GetFileContent or
	// by a GetFileContents chunk, so the fetched-set assertions hold for both
	// read shapes. batchCalls holds the paths of each GetFileContents call.
	getCalls   []string
	batchCalls [][]string
	// singleCalls counts GetFileContent statements only.
	singleCalls int
	blankCalls  [][]string
	listCalls   int
	bytes       int
}

func newHydrationStore(files ...querycontract.FileContent) *hydrationStore {
	store := &hydrationStore{
		rows:  make(map[string]querycontract.FileContent, len(files)),
		gone:  map[string]bool{},
		getAt: map[string]error{},

		goneAfterCheck: map[string]bool{},
	}
	for _, file := range files {
		file.RepoID = hydrationTestRepoID
		store.rows[file.RelativePath] = file
	}
	return store
}

func (s *hydrationStore) ListRepoFiles(_ context.Context, _ string, limit int) ([]querycontract.FileContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listCalls++
	paths := make([]string, 0, len(s.rows))
	for relativePath := range s.rows {
		paths = append(paths, relativePath)
	}
	sort.Strings(paths)
	files := make([]querycontract.FileContent, 0, len(paths))
	for _, relativePath := range paths {
		row := s.rows[relativePath]
		row.Content = ""
		files = append(files, row)
		if limit > 0 && len(files) >= limit {
			break
		}
	}
	return files, nil
}

func (s *hydrationStore) GetFileContent(_ context.Context, repoID, relativePath string) (*querycontract.FileContent, error) {
	if repoID != hydrationTestRepoID {
		return nil, fmt.Errorf("content read for repository %q, want %q", repoID, hydrationTestRepoID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.getCalls = append(s.getCalls, relativePath)
	s.singleCalls++
	if err := s.getAt[relativePath]; err != nil {
		return nil, err
	}
	row, ok := s.rows[relativePath]
	if !ok || s.gone[relativePath] || s.goneAfterCheck[relativePath] {
		return nil, nil
	}
	s.bytes += len(row.Content)
	return &row, nil
}

// GetFileContents answers a chunk from the same rows and rules as
// GetFileContent: a vanished row is absent, a path in getAt fails the whole
// call, and the rows are returned in request order without duplicates.
func (s *hydrationStore) GetFileContents(_ context.Context, repoID string, relativePaths []string) ([]querycontract.FileContent, error) {
	if repoID != hydrationTestRepoID {
		return nil, fmt.Errorf("content read for repository %q, want %q", repoID, hydrationTestRepoID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchCalls = append(s.batchCalls, append([]string(nil), relativePaths...))
	s.getCalls = append(s.getCalls, relativePaths...)
	for _, relativePath := range relativePaths {
		if err := s.getAt[relativePath]; err != nil {
			return nil, err
		}
	}
	seen := make(map[string]struct{}, len(relativePaths))
	out := make([]querycontract.FileContent, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		if _, dup := seen[relativePath]; dup {
			continue
		}
		seen[relativePath] = struct{}{}
		row, ok := s.rows[relativePath]
		if !ok || s.gone[relativePath] || s.goneAfterCheck[relativePath] {
			continue
		}
		s.bytes += len(row.Content)
		out = append(out, row)
	}
	return out, nil
}

func (s *hydrationStore) NonBlankRepoFilePaths(_ context.Context, _ string, relativePaths []string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blankCalls = append(s.blankCalls, append([]string(nil), relativePaths...))
	if s.blankErr != nil {
		return nil, s.blankErr
	}
	nonBlank := func(content string) bool { return strings.TrimSpace(content) != "" }
	if s.blankMode != nil {
		nonBlank = s.blankMode
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		row, ok := s.rows[relativePath]
		if !ok || s.gone[relativePath] || !nonBlank(row.Content) {
			continue
		}
		if _, dup := seen[relativePath]; dup {
			continue
		}
		seen[relativePath] = struct{}{}
		out = append(out, relativePath)
	}
	return out, nil
}

// listed returns the metadata-only list the loader would read for the store.
func (s *hydrationStore) listed(t testing.TB) []querycontract.FileContent {
	t.Helper()
	files, err := s.ListRepoFiles(context.Background(), hydrationTestRepoID, 5000)
	if err != nil {
		t.Fatalf("ListRepoFiles() error = %v", err)
	}
	s.mu.Lock()
	s.listCalls--
	s.mu.Unlock()
	return files
}

// fullConfigHydration is the read-everything pass the loader ran before
// #7126: hydrate the first 50 candidates in full, then keep candidates with
// non-blank content. It is the equality oracle.
func fullConfigHydration(
	ctx context.Context,
	reader querycontract.ContentStore,
	repoID string,
	files []querycontract.FileContent,
	_ *ConfigArtifactReadStats,
) ([]querycontract.FileContent, error) {
	hydrated, err := HydrateRepositoryCandidateFiles(ctx, reader, repoID, files, isConfigArtifactCandidate)
	if err != nil {
		return nil, err
	}
	contentFiles := make([]querycontract.FileContent, 0, len(hydrated))
	for _, file := range hydrated {
		if !isConfigArtifactCandidate(file) {
			continue
		}
		if strings.TrimSpace(file.Content) == "" {
			continue
		}
		contentFiles = append(contentFiles, file)
	}
	return contentFiles, nil
}

var fullConfigArtifactPasses = configArtifactPasses{
	hydrate: fullConfigHydration,
	build:   buildRepositoryConfigArtifacts,
}

type loadOutcome struct {
	digest string
	rows   int
	err    error
	stats  ConfigArtifactReadStats
}

// runConfigLoad runs the real loader over one anchor source (file list given,
// like the repository story) and one related source (file list read from the
// store, like a related repository), and digests the merged config_paths.
func runConfigLoad(
	t testing.TB,
	store *hydrationStore,
	anchorFiles []querycontract.FileContent,
	passes configArtifactPasses,
) loadOutcome {
	t.Helper()
	var stats ConfigArtifactReadStats
	sources := []repositoryArtifactSource{
		{RepoID: hydrationTestRepoID, RepoName: "svc", Files: anchorFiles, HasFileList: true},
		{RepoID: hydrationTestRepoID, RepoName: "related"},
	}
	result, err := loadRepositoryConfigArtifactsForSources(context.Background(), store, sources, &stats, passes)
	outcome := loadOutcome{err: err, stats: stats}
	if err != nil {
		return outcome
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("marshal config artifacts: %v", marshalErr)
	}
	sum := sha256.Sum256(encoded)
	outcome.digest = hex.EncodeToString(sum[:])
	outcome.rows = len(querycontract.MapSliceValue(result, "config_paths"))
	return outcome
}

// compareHydrationPasses loads the same store twice, once with the oracle and
// once with candidate, and reports whether the output and the error agree.
// The two runs read separate stores built by newStore, so recorded statements
// stay per run.
func compareHydrationPasses(
	t testing.TB,
	newStore func() *hydrationStore,
	preloadAnchor bool,
	candidate configArtifactPasses,
) (oracle, got loadOutcome, oracleStore, gotStore *hydrationStore, equal bool) {
	t.Helper()
	oracleStore = newStore()
	gotStore = newStore()
	var oracleFiles, gotFiles []querycontract.FileContent
	if preloadAnchor {
		oracleFiles = oracleStore.listed(t)
		gotFiles = gotStore.listed(t)
	}
	oracle = runConfigLoad(t, oracleStore, oracleFiles, fullConfigArtifactPasses)
	got = runConfigLoad(t, gotStore, gotFiles, candidate)
	equal = oracle.digest == got.digest && (oracle.err == nil) == (got.err == nil)
	return oracle, got, oracleStore, gotStore, equal
}

func requireSameConfigOutput(
	t testing.TB,
	name string,
	newStore func() *hydrationStore,
	candidate configArtifactPasses,
) (oracle, got loadOutcome, oracleStore, gotStore *hydrationStore) {
	t.Helper()
	oracle, got, oracleStore, gotStore, equal := compareHydrationPasses(t, newStore, true, candidate)
	if !equal {
		t.Fatalf("%s: needed-content output differs from full hydration: oracle rows=%d err=%v digest=%s, got rows=%d err=%v digest=%s",
			name, oracle.rows, oracle.err, oracle.digest, got.rows, got.err, got.digest)
	}
	return oracle, got, oracleStore, gotStore
}

var errInjectedRead = errors.New("injected content read failure")

func fileRow(relativePath, content string) querycontract.FileContent {
	return querycontract.FileContent{RelativePath: relativePath, Content: content}
}

func kustomizationFor(refs ...string) string {
	var builder strings.Builder
	builder.WriteString("apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n")
	for _, ref := range refs {
		fmt.Fprintf(&builder, "  - %s\n", ref)
	}
	return builder.String()
}

const policyResourceYAML = `apiVersion: iam.aws.upbound.io/v1beta1
kind: RolePolicy
spec:
  policyDocument:
    Statement:
      - Resource:
          - arn:aws:ssm:us-east-1:123456789012:parameter/configd/%s/*
`

func policyResource(name string) string { return fmt.Sprintf(policyResourceYAML, name) }
