package query

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestSearchConsumerEvidenceAnyRepoStartsBoundedSearchesConcurrently(t *testing.T) {
	t.Parallel()

	store := &blockingConsumerSearchContentStore{
		started: make(chan string, 3),
		release: make(chan struct{}),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := searchConsumerEvidenceAnyRepo(
			ctx,
			store,
			"repo-sample-service-api",
			"sample-service-api",
			[]string{"sample-service.dev.example.test", "sample-service.qa.example.test"},
			25,
		)
		done <- err
	}()

	seen := map[string]struct{}{}
	for len(seen) < 3 {
		select {
		case term := <-store.started:
			seen[term] = struct{}{}
		case <-time.After(300 * time.Millisecond):
			t.Fatalf("started searches = %#v, want service-name and hostname searches to start before the first one completes", seen)
		}
	}
	close(store.release)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("searchConsumerEvidenceAnyRepo() error = %v, want nil", err)
		}
	case <-time.After(time.Second):
		t.Fatal("searchConsumerEvidenceAnyRepo() did not complete after releasing blocked searches")
	}
}

func TestSearchConsumerEvidenceAnyRepoUsesIndexedServiceReferences(t *testing.T) {
	t.Parallel()

	store := &indexedReferenceConsumerSearchContentStore{
		referenceRows: map[string][]FileContent{
			"service_name:sample-service-api": {
				{RepoID: "repo-consumer", RelativePath: "deploy/values.yaml"},
			},
		},
	}
	got, err := searchConsumerEvidenceAnyRepo(
		context.Background(),
		store,
		"repo-sample-service-api",
		"sample-service-api",
		nil,
		25,
	)
	if err != nil {
		t.Fatalf("searchConsumerEvidenceAnyRepo() error = %v, want nil", err)
	}
	if got, want := store.referenceCalls, 1; got != want {
		t.Fatalf("referenceCalls = %d, want %d", got, want)
	}
	if got, want := store.insensitiveCalls, 0; got != want {
		t.Fatalf("insensitiveCalls = %d, want %d when indexed references are available", got, want)
	}
	if got, want := store.exactCalls, 0; got != want {
		t.Fatalf("exactCalls = %d, want %d after indexed service search", got, want)
	}
	if _, ok := got["repo-consumer"]; !ok {
		t.Fatalf("evidence repos = %#v, want repo-consumer", got)
	}
}

func TestSearchConsumerEvidenceAnyRepoFallsBackWhenIndexedServiceMissing(t *testing.T) {
	t.Parallel()

	store := &indexedReferenceConsumerSearchContentStore{
		exactRows: []FileContent{
			{RepoID: "repo-consumer", RelativePath: "deploy/values.yaml"},
		},
	}
	got, err := searchConsumerEvidenceAnyRepo(
		context.Background(),
		store,
		"repo-sample-service-api",
		"sample-service-api",
		nil,
		25,
	)
	if err != nil {
		t.Fatalf("searchConsumerEvidenceAnyRepo() error = %v, want nil", err)
	}
	if got, want := store.referenceCalls, 1; got != want {
		t.Fatalf("referenceCalls = %d, want %d", got, want)
	}
	if got, want := store.exactCalls, 1; got != want {
		t.Fatalf("exactCalls = %d, want %d after empty indexed service lookup", got, want)
	}
	if _, ok := got["repo-consumer"]; !ok {
		t.Fatalf("evidence repos = %#v, want repo-consumer", got)
	}
}

func TestSearchConsumerEvidenceAnyRepoUsesIndexedHostnameReferences(t *testing.T) {
	t.Parallel()

	store := &indexedHostnameConsumerSearchContentStore{
		referenceRows: []FileContent{
			{RepoID: "repo-consumer", RelativePath: "deploy/values.yaml"},
		},
	}
	got, err := searchConsumerEvidenceAnyRepo(
		context.Background(),
		store,
		"repo-sample-service-api",
		"",
		[]string{"sample-service-api.qa.example.test"},
		25,
	)
	if err != nil {
		t.Fatalf("searchConsumerEvidenceAnyRepo() error = %v, want nil", err)
	}
	if got, want := store.referenceCalls, 1; got != want {
		t.Fatalf("referenceCalls = %d, want %d", got, want)
	}
	if got, want := store.exactCalls, 0; got != want {
		t.Fatalf("exactCalls = %d, want %d when indexed references are available", got, want)
	}
	if _, ok := got["repo-consumer"]; !ok {
		t.Fatalf("evidence repos = %#v, want repo-consumer", got)
	}
}

func TestSearchConsumerEvidenceAnyRepoFallsBackWhenIndexedHostnameMissing(t *testing.T) {
	t.Parallel()

	store := &indexedHostnameConsumerSearchContentStore{
		exactRows: []FileContent{
			{RepoID: "repo-consumer", RelativePath: "deploy/values.yaml"},
		},
	}
	got, err := searchConsumerEvidenceAnyRepo(
		context.Background(),
		store,
		"repo-sample-service-api",
		"",
		[]string{"sample-service-api.qa.example.test"},
		25,
	)
	if err != nil {
		t.Fatalf("searchConsumerEvidenceAnyRepo() error = %v, want nil", err)
	}
	if got, want := store.referenceCalls, 1; got != want {
		t.Fatalf("referenceCalls = %d, want %d", got, want)
	}
	if got, want := store.exactCalls, 1; got != want {
		t.Fatalf("exactCalls = %d, want %d after empty indexed lookup", got, want)
	}
	if _, ok := got["repo-consumer"]; !ok {
		t.Fatalf("evidence repos = %#v, want repo-consumer", got)
	}
}

func TestSearchConsumerEvidenceAnyRepoKeepsInsensitiveSearchForMixedCaseServiceToken(t *testing.T) {
	t.Parallel()

	store := &methodChoiceConsumerSearchContentStore{}
	_, err := searchConsumerEvidenceAnyRepo(
		context.Background(),
		store,
		"repo-sample-service-api",
		"Sample-Service-API",
		nil,
		25,
	)
	if err != nil {
		t.Fatalf("searchConsumerEvidenceAnyRepo() error = %v, want nil", err)
	}
	if got, want := store.insensitiveCalls, 1; got != want {
		t.Fatalf("insensitiveCalls = %d, want %d for mixed-case service token", got, want)
	}
	if got, want := store.exactCalls, 0; got != want {
		t.Fatalf("exactCalls = %d, want %d for mixed-case service token", got, want)
	}
}

type indexedReferenceConsumerSearchContentStore struct {
	methodChoiceConsumerSearchContentStore
	referenceCalls int
	referenceRows  map[string][]FileContent
	exactRows      []FileContent
}

func (s *indexedReferenceConsumerSearchContentStore) SearchFileReferenceAnyRepo(_ context.Context, kind string, value string, _ int) ([]FileContent, bool, error) {
	s.referenceCalls++
	key := kind + ":" + value
	rows := append([]FileContent(nil), s.referenceRows[key]...)
	return rows, len(rows) > 0, nil
}

func (s *indexedReferenceConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]FileContent, error) {
	s.exactCalls++
	return append([]FileContent(nil), s.exactRows...), nil
}

type indexedHostnameConsumerSearchContentStore struct {
	fakePortContentStore
	referenceCalls int
	exactCalls     int
	referenceRows  []FileContent
	exactRows      []FileContent
}

func (s *indexedHostnameConsumerSearchContentStore) SearchFileReferenceAnyRepo(_ context.Context, kind string, value string, _ int) ([]FileContent, bool, error) {
	s.referenceCalls++
	if kind != "hostname" {
		return nil, true, nil
	}
	if value != "sample-service-api.qa.example.test" {
		return nil, true, nil
	}
	rows := append([]FileContent(nil), s.referenceRows...)
	return rows, len(rows) > 0, nil
}

func (s *indexedHostnameConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]FileContent, error) {
	s.exactCalls++
	return append([]FileContent(nil), s.exactRows...), nil
}

type methodChoiceConsumerSearchContentStore struct {
	fakePortContentStore
	insensitiveCalls int
	exactCalls       int
}

func (s *methodChoiceConsumerSearchContentStore) SearchFileContentAnyRepo(context.Context, string, int) ([]FileContent, error) {
	s.insensitiveCalls++
	return nil, nil
}

func (s *methodChoiceConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(context.Context, string, int) ([]FileContent, error) {
	s.exactCalls++
	return nil, nil
}

type blockingConsumerSearchContentStore struct {
	fakePortContentStore
	started chan string
	release chan struct{}
	mu      sync.Mutex
	calls   []string
}

func (s *blockingConsumerSearchContentStore) SearchFileContentAnyRepo(ctx context.Context, pattern string, _ int) ([]FileContent, error) {
	return s.blockedSearch(ctx, pattern)
}

func (s *blockingConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(ctx context.Context, pattern string, _ int) ([]FileContent, error) {
	return s.blockedSearch(ctx, pattern)
}

func (s *blockingConsumerSearchContentStore) blockedSearch(ctx context.Context, pattern string) ([]FileContent, error) {
	s.mu.Lock()
	s.calls = append(s.calls, pattern)
	s.mu.Unlock()
	select {
	case s.started <- pattern:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case <-s.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type patternConsumerSearchContentStore struct {
	fakePortContentStore
	fileRows  map[string][]FileContent
	exactRows map[string][]FileContent
}

func (s patternConsumerSearchContentStore) SearchFileContentAnyRepo(_ context.Context, pattern string, _ int) ([]FileContent, error) {
	return append([]FileContent(nil), s.fileRows[pattern]...), nil
}

func (s patternConsumerSearchContentStore) SearchFileContentAnyRepoExactCase(_ context.Context, pattern string, _ int) ([]FileContent, error) {
	rows := s.exactRows[pattern]
	if len(rows) == 0 {
		rows = s.fileRows[pattern]
	}
	return append([]FileContent(nil), rows...), nil
}
