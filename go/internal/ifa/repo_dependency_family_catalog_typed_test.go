// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"bytes"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/replay"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	codegraphv1 "github.com/eshu-hq/eshu/sdk/go/factschema/codegraph/v1"
)

var repoDependencyFamilyTypedRepositoryBuilder func(codegraphv1.Repository) facts.Envelope = repoDependencyFamilyRepositoryFact

func TestRepoDependencyFamilyRepositoryBuilderUsesCanonicalTypedContract(t *testing.T) {
	t.Parallel()

	graphID := "repo-typed-contract"
	graphKind := "repository"
	name := "typed-contract"
	parsedFileCount := "17"
	isDependency := true
	repoSlug := "ifa-org/typed-contract"
	remoteURL := "https://github.com/ifa-org/typed-contract.git"
	localPath := "/fixtures/ifa/typed-contract"
	defaultBranch := "main"
	deltaGeneration := true
	reconciliationGeneration := true
	sourceRunID := "run-typed-contract"
	repository := codegraphv1.Repository{
		RepoID:  repoDependencyFamilyTargetDependsOnRepoID,
		GraphID: &graphID, GraphKind: &graphKind, Name: &name,
		ParsedFileCount: &parsedFileCount, IsDependency: &isDependency,
		RepoSlug: &repoSlug, RemoteURL: &remoteURL, LocalPath: &localPath,
		DefaultBranch:   &defaultBranch,
		GitRefs:         []codegraphv1.GitRef{{Name: "main", Kind: "branch", HeadSHA: repoDependencyFamilyCommitSHA, IsDefault: true}},
		DeltaGeneration: &deltaGeneration, ReconciliationGeneration: &reconciliationGeneration,
		DeltaRelativePaths: []string{"changed.go"}, DeltaDeletedRelativePaths: []string{"deleted.go"},
		SourceRunID: &sourceRunID,
	}

	wantPayload, err := factschema.EncodeCodegraphRepository(repository)
	if err != nil {
		t.Fatalf("EncodeCodegraphRepository(want): %v", err)
	}
	fact := repoDependencyFamilyTypedRepositoryBuilder(repository)
	decoded, err := factschema.DecodeCodegraphRepository(factschema.Envelope{
		SchemaVersion: fact.SchemaVersion,
		Payload:       fact.Payload,
	})
	if err != nil {
		t.Fatalf("DecodeCodegraphRepository(builder payload): %v", err)
	}
	reencoded, err := factschema.EncodeCodegraphRepository(decoded)
	if err != nil {
		t.Fatalf("EncodeCodegraphRepository(decoded payload): %v", err)
	}

	wantCanonical := canonicalRepositoryPayload(t, wantPayload)
	if got := canonicalRepositoryPayload(t, fact.Payload); !bytes.Equal(got, wantCanonical) {
		t.Fatalf("builder payload drifted from typed encoder\ngot:  %s\nwant: %s", got, wantCanonical)
	}
	if got := canonicalRepositoryPayload(t, reencoded); !bytes.Equal(got, wantCanonical) {
		t.Fatalf("typed round trip drifted\ngot:  %s\nwant: %s", got, wantCanonical)
	}
}

func canonicalRepositoryPayload(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	canonical, err := replay.CanonicalizeValue(payload, replay.CanonicalOptions{})
	if err != nil {
		t.Fatalf("canonicalize repository payload: %v", err)
	}
	return canonical
}
