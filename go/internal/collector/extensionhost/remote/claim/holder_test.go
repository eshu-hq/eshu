// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// Single-holder producer authority: a grant authorizes exactly the producer
// identity the client was constructed with. A live grant naming any other
// producer, version, or undeclared scope never authorizes this holder, and
// the holder identity never comes from the wire: AuthorizeEmission takes no
// producer argument, so a tampered server response cannot reassign it.

func TestHolder_GrantForAnotherProducerDenies(t *testing.T) {
	t.Parallel()

	other := liveGrant()
	other.ProducerID = "some-other-producer"
	live := &mutableGrants{grants: []component.ProducerGrant{other}}
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError despite a live grant", err, err)
	}
	if deny.Reason != component.GrantReasonNoMatchingGrant {
		t.Fatalf("reason = %q, want no_matching_grant: a neighbor's grant is not this holder's", deny.Reason)
	}
}

func TestHolder_VersionPinningBindsTheHolder(t *testing.T) {
	t.Parallel()

	other := liveGrant()
	other.Version = "2.0.0"
	live := &mutableGrants{grants: []component.ProducerGrant{other}}
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError for a version mismatch", err, err)
	}
	if deny.Reason != component.GrantReasonNoMatchingGrant {
		t.Fatalf("reason = %q, want no_matching_grant", deny.Reason)
	}
}

func TestHolder_VersionSpellingIsNormalized(t *testing.T) {
	t.Parallel()

	// The v prefix is not part of grant identity (NormalizeGrantVersion):
	// both spellings name the same holder.
	grant := liveGrant()
	grant.Version = "v0.1.0"
	live := &mutableGrants{grants: []component.ProducerGrant{grant}}
	client := replayClient(t, "http://localhost:1", live, nil)

	if err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema); err != nil {
		t.Fatalf("AuthorizeEmission() = %v, want allow across the v-prefix spelling", err)
	}
}

func TestHolder_UndeclaredScopeDenies(t *testing.T) {
	t.Parallel()

	grant := liveGrant()
	grant.Scope = "not-a-declared-kind"
	live := &mutableGrants{grants: []component.ProducerGrant{grant}}
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError", err, err)
	}
	if deny.Reason != component.GrantReasonScopeMismatch {
		t.Fatalf("reason = %q, want scope_mismatch", deny.Reason)
	}
}

func TestHolder_UncoveredSchemaDenies(t *testing.T) {
	t.Parallel()

	grant := liveGrant()
	grant.SchemaVersions = []string{"9.9.9"}
	live := &mutableGrants{grants: []component.ProducerGrant{grant}}
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError", err, err)
	}
	if deny.Reason != component.GrantReasonSchemaNotCovered {
		t.Fatalf("reason = %q, want schema_not_covered", deny.Reason)
	}
}

func TestHolder_DenyErrorNamesIdentifiersOnly(t *testing.T) {
	t.Parallel()

	revoked := liveGrant()
	revoked.Revoked = true
	live := &mutableGrants{grants: []component.ProducerGrant{revoked}}
	client := replayClient(t, "http://localhost:1", live, nil)

	err := client.AuthorizeEmission(context.Background(), replayCoreKind, replayCoreSchema)
	var deny *DenyError
	if !errors.As(err, &deny) {
		t.Fatalf("AuthorizeEmission() = %v (%T), want *DenyError", err, err)
	}
	if deny.ProducerID != testProducerID || deny.Version != testProducerVersion || deny.Kind != replayCoreKind {
		t.Fatalf("deny = %+v, want the holder identifiers and kind only", deny)
	}
}
