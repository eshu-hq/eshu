// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeRoutedSource struct {
	scopeID string
	called  int
}

func (f *fakeRoutedSource) NextClaimed(
	context.Context,
	workflow.WorkItem,
) (collector.CollectedGeneration, bool, error) {
	f.called++
	return collector.CollectedGeneration{
		Scope: scope.IngestionScope{ScopeID: f.scopeID},
	}, true, nil
}

func TestNewProviderRoutedSourceRejectsEmptyRegistrations(t *testing.T) {
	t.Parallel()

	if _, err := newProviderRoutedSource(nil); err == nil {
		t.Fatal("newProviderRoutedSource(nil) error = nil, want non-nil")
	}
	if _, err := newProviderRoutedSource(map[string]collector.ClaimedSource{}); err == nil {
		t.Fatal("newProviderRoutedSource({}) error = nil, want non-nil")
	}
}

func TestProviderRoutedSourceDispatchesByScopeID(t *testing.T) {
	t.Parallel()

	github := &fakeRoutedSource{scopeID: "ci-cd:github-actions:example/repo"}
	gitlab := &fakeRoutedSource{scopeID: "gitlab-ci://gitlab.com/eshu-hq/demo"}
	routed, err := newProviderRoutedSource(map[string]collector.ClaimedSource{
		"ci-cd:github-actions:example/repo":   github,
		"gitlab-ci://gitlab.com/eshu-hq/demo": gitlab,
	})
	if err != nil {
		t.Fatalf("newProviderRoutedSource() error = %v, want nil", err)
	}

	collected, ok, err := routed.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID: "gitlab-ci://gitlab.com/eshu-hq/demo",
	})
	if err != nil || !ok {
		t.Fatalf("NextClaimed(gitlab) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if got, want := collected.Scope.ScopeID, "gitlab-ci://gitlab.com/eshu-hq/demo"; got != want {
		t.Fatalf("Scope.ScopeID = %q, want %q", got, want)
	}
	if github.called != 0 {
		t.Fatalf("github source called = %d, want 0 (gitlab claim must not reach it)", github.called)
	}
	if gitlab.called != 1 {
		t.Fatalf("gitlab source called = %d, want 1", gitlab.called)
	}

	_, ok, err = routed.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID: "ci-cd:github-actions:example/repo",
	})
	if err != nil || !ok {
		t.Fatalf("NextClaimed(github) = (_, %v, %v), want (_, true, nil)", ok, err)
	}
	if github.called != 1 {
		t.Fatalf("github source called = %d, want 1", github.called)
	}
}

func TestProviderRoutedSourceRejectsUnregisteredScope(t *testing.T) {
	t.Parallel()

	routed, err := newProviderRoutedSource(map[string]collector.ClaimedSource{
		"ci-cd:github-actions:example/repo": &fakeRoutedSource{},
	})
	if err != nil {
		t.Fatalf("newProviderRoutedSource() error = %v, want nil", err)
	}
	_, _, err = routed.NextClaimed(context.Background(), workflow.WorkItem{
		ScopeID: "gitlab-ci://gitlab.com/unregistered/project",
	})
	if err == nil {
		t.Fatal("NextClaimed() error = nil, want unregistered-scope error")
	}
}
