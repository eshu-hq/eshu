// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

const seededCredential = "s3cr3t-credential-value"

func markerStore() parser.ContentStore {
	return parser.ContentStore{
		Policy:         parser.ContentStoreRedact,
		Redactor:       parser.RedactorMarker,
		CredentialKeys: []string{"npmAuthToken"},
	}
}

func TestApplyContentStoreStoreKeepsTheBody(t *testing.T) {
	t.Parallel()

	for _, store := range []parser.ContentStore{{}, {Policy: parser.ContentStoreStore}} {
		got := ApplyContentStore(store, "npmAuthToken: "+seededCredential+"\n")
		if got.Body != "npmAuthToken: "+seededCredential+"\n" || got.Outcome != StoreOutcomeStored {
			t.Fatalf("store policy changed the body: %+v", got)
		}
	}
}

func TestApplyContentStoreMetadataNeverWritesABody(t *testing.T) {
	t.Parallel()

	bodies := []string{"", "x", "npmAuthToken: " + seededCredential + "\n", "\xff\xfe", strings.Repeat("line\n", 1000)}
	for _, body := range bodies {
		got := ApplyContentStore(parser.ContentStore{Policy: parser.ContentStoreMetadata}, body)
		if got.Body != "" {
			t.Fatalf("metadata policy returned a %d-byte body for %q", len(got.Body), body)
		}
		if got.Outcome != StoreOutcomeMetadata {
			t.Fatalf("outcome = %q, want metadata", got.Outcome)
		}
	}
}

func TestApplyContentStoreRedactMarkerRedactsAndCounts(t *testing.T) {
	t.Parallel()

	got := ApplyContentStore(markerStore(), "npmAuthToken: "+seededCredential+"\nnpmRegistryServer: x\n")
	if strings.Contains(got.Body, seededCredential) {
		t.Fatalf("seeded credential survived: %q", got.Body)
	}
	if got.Body != "npmAuthToken: REDACTED_api_token\nnpmRegistryServer: x\n" || got.Outcome != StoreOutcomeRedacted || got.Replaced != 1 {
		t.Fatalf("got %+v", got)
	}
	clean := ApplyContentStore(markerStore(), "nodeLinker: node-modules\n")
	if clean.Body != "nodeLinker: node-modules\n" || clean.Outcome != StoreOutcomeClean || clean.Replaced != 0 {
		t.Fatalf("clean body: %+v", clean)
	}
}

// TestApplyContentStoreFailsClosed pins the no-raw-fallback rule: a redactor
// error, non-UTF-8 input, and a policy the collector does not know all produce
// an empty-content metadata result carrying the failure, never the source body.
func TestApplyContentStoreFailsClosed(t *testing.T) {
	t.Parallel()

	failing := func(string, parser.ContentStore) (string, int, error) {
		return seededCredential, 1, errors.New("injected redactor failure")
	}
	cases := map[string]StoreResult{
		"redactor error": applyContentStore(markerStore(), "npmAuthToken: "+seededCredential, failing),
		"non-UTF-8":      ApplyContentStore(markerStore(), "npmAuthToken: "+seededCredential+"\xff\n"),
		"unknown policy": ApplyContentStore(parser.ContentStore{Policy: "unheard-of"}, seededCredential),
		"unknown mode": ApplyContentStore(
			parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: "bogus", CredentialKeys: []string{"k"}},
			seededCredential),
	}
	for name, got := range cases {
		if got.Body != "" {
			t.Errorf("%s: body %q reached the store; no raw fallback is allowed", name, got.Body)
		}
		if got.Outcome != StoreOutcomeRedactorFailed || got.Err == nil {
			t.Errorf("%s: outcome %q err %v, want redactor_failed with an error", name, got.Outcome, got.Err)
		}
	}
}

// TestApplyContentStoreNamesTheFailureReason pins the bounded reason class an
// operator reads in the WARN log. The class never carries error text.
func TestApplyContentStoreNamesTheFailureReason(t *testing.T) {
	t.Parallel()

	failing := func(string, parser.ContentStore) (string, int, error) {
		return "", 0, errors.New("boom " + seededCredential)
	}
	panicking := func(string, parser.ContentStore) (string, int, error) { panic("boom " + seededCredential) }
	cases := map[string]struct {
		got  StoreResult
		want FailureReason
	}{
		"invalid utf-8":  {ApplyContentStore(markerStore(), "npmAuthToken: \xff\n"), FailureInvalidUTF8},
		"nul":            {ApplyContentStore(markerStore(), "a\x00b"), FailureNotText},
		"too large":      {ApplyContentStore(markerStore(), strings.Repeat("a", MaxGovernedBodyBytes+1)), FailureTooLarge},
		"redactor error": {applyContentStore(markerStore(), "x", failing), FailureRedactorError},
		"redactor panic": {applyContentStore(markerStore(), "x", panicking), FailureRedactorPanic},
		"unknown policy": {ApplyContentStore(parser.ContentStore{Policy: "other"}, "x"), FailureUnknownPolicy},
		"unknown mode": {ApplyContentStore(parser.ContentStore{
			Policy: parser.ContentStoreRedact, Redactor: "bogus", CredentialKeys: []string{"k"},
		}, "x"), FailureUnknownMode},
	}
	for name, tc := range cases {
		if tc.got.Reason != tc.want {
			t.Errorf("%s: reason = %q, want %q", name, tc.got.Reason, tc.want)
		}
		if strings.Contains(string(tc.got.Reason), seededCredential) {
			t.Errorf("%s: the reason class carries error text", name)
		}
	}
	if ok := ApplyContentStore(markerStore(), "x: y\n"); ok.Reason != "" {
		t.Errorf("a successful result carries reason %q", ok.Reason)
	}
}

func skeletonStore() parser.ContentStore {
	return parser.ContentStore{Policy: parser.ContentStoreRedact, Redactor: parser.RedactorSkeleton}
}

// TestApplyContentStoreRedactSkeleton routes the skeleton mode to the skeleton
// redactor (#8833) and fails closed on non-text input like the marker mode.
func TestApplyContentStoreRedactSkeleton(t *testing.T) {
	t.Parallel()

	got := ApplyContentStore(skeletonStore(), "db_password = \""+seededCredential+"\"\nreplicas = 2\n")
	if got.Body != "db_password = REDACTED_password_literal\nreplicas = 2\n" || got.Outcome != StoreOutcomeRedacted || got.Replaced != 1 {
		t.Fatalf("got %+v", got)
	}
	clean := ApplyContentStore(skeletonStore(), "replicas = 2\n")
	if clean.Body != "replicas = 2\n" || clean.Outcome != StoreOutcomeClean {
		t.Fatalf("clean body: %+v", clean)
	}
	failed := ApplyContentStore(skeletonStore(), "a = \""+seededCredential+"\xff\"\n")
	if failed.Body != "" || failed.Outcome != StoreOutcomeRedactorFailed || failed.Reason != FailureInvalidUTF8 {
		t.Fatalf("non-UTF-8 skeleton body: %+v", failed)
	}
}

// TestApplyContentStorePassesURLKeysToTheMarker checks the definition's URL
// key list reaches the marker redactor.
func TestApplyContentStorePassesURLKeysToTheMarker(t *testing.T) {
	t.Parallel()

	store := markerStore()
	store.URLUserinfoKeys = []string{"npmRegistryServer"}
	got := ApplyContentStore(store, "npmRegistryServer: \"https://u:"+seededCredential+"@npm.example.com/\"\n")
	if got.Body != "npmRegistryServer: \"https://REDACTED_password_literal@npm.example.com/\"\n" || got.Outcome != StoreOutcomeRedacted {
		t.Fatalf("got %+v", got)
	}
}
