// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerQueueAckBindsContainerImageIdentityClaimEpoch(t *testing.T) {
	t.Parallel()

	db := &fakeExecQueryer{}
	queue := ReducerQueue{
		db:            db,
		LeaseOwner:    "reducer-5854",
		LeaseDuration: time.Minute,
	}
	if err := queue.Ack(
		context.Background(),
		reducer.Intent{
			IntentID:     "intent-5854-single",
			Domain:       reducer.DomainContainerImageIdentity,
			AttemptCount: 7,
			ClaimEpoch:   71,
		},
		reducer.Result{},
	); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("Ack() exec count = %d, want %d", got, want)
	}
	assertContainerImageIdentityAttemptBoundAckQuery(t, db.execs[0].query)
	if got, want := len(db.execs[0].args), 4; got != want {
		t.Fatalf("target ACK arg count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[3], int64(71); got != want {
		t.Fatalf("target ACK claim epoch arg = %#v, want %#v", got, want)
	}
}

func TestReducerQueueAckLeavesUnrelatedAndEmptyDomainsOnLegacyQuery(t *testing.T) {
	t.Parallel()

	for _, domain := range []reducer.Domain{reducer.DomainOwnership, ""} {
		db := &fakeExecQueryer{}
		queue := ReducerQueue{
			db:            db,
			LeaseOwner:    "reducer-5854",
			LeaseDuration: time.Minute,
		}
		if err := queue.Ack(
			context.Background(),
			reducer.Intent{
				IntentID: "intent-5854-legacy-dispatch",
				Domain:   domain,
			},
			reducer.Result{},
		); err != nil {
			t.Fatalf("Ack(domain=%q) error = %v", domain, err)
		}
		if got, want := len(db.execs), 1; got != want {
			t.Fatalf("Ack(domain=%q) exec count = %d, want %d", domain, got, want)
		}
		assertContainerImageIdentityLegacyAckQuery(t, db.execs[0].query)
	}
}

func TestReducerQueueAckBatchBindsContainerImageIdentityClaimEpochs(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name        string
		intents     []reducer.Intent
		wantCapable bool
		wantMixed   bool
	}{
		{
			name: "target only",
			intents: []reducer.Intent{
				{
					IntentID:     "intent-5854-batch-target-a",
					Domain:       reducer.DomainContainerImageIdentity,
					AttemptCount: 3,
					ClaimEpoch:   31,
				},
				{
					IntentID:     "intent-5854-batch-target-b",
					Domain:       reducer.DomainContainerImageIdentity,
					AttemptCount: 3,
					ClaimEpoch:   32,
				},
			},
			wantCapable: true,
		},
		{
			name: "mixed",
			intents: []reducer.Intent{
				{
					IntentID:     "intent-5854-batch-mixed-target",
					Domain:       reducer.DomainContainerImageIdentity,
					AttemptCount: 7,
					ClaimEpoch:   71,
				},
				{
					IntentID:     "intent-5854-batch-mixed-unrelated",
					Domain:       reducer.DomainOwnership,
					AttemptCount: 9,
					ClaimEpoch:   0,
				},
			},
			wantCapable: true,
			wantMixed:   true,
		},
		{
			name: "unrelated only",
			intents: []reducer.Intent{
				{
					IntentID: "intent-5854-batch-unrelated-a",
					Domain:   reducer.DomainOwnership,
				},
				{
					IntentID: "intent-5854-batch-unrelated-b",
					Domain:   reducer.DomainGovernance,
				},
			},
		},
		{
			name: "empty domains",
			intents: []reducer.Intent{
				{IntentID: "intent-5854-batch-empty-a"},
				{IntentID: "intent-5854-batch-empty-b"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			db := &fakeExecQueryer{}
			queue := ReducerQueue{
				db:            db,
				LeaseOwner:    "reducer-5854",
				LeaseDuration: time.Minute,
			}
			if err := queue.AckBatch(context.Background(), test.intents, nil); err != nil {
				t.Fatalf("AckBatch() error = %v", err)
			}
			wantExecs := 1
			if test.wantMixed {
				wantExecs = 2
			}
			if got, want := len(db.execs), wantExecs; got != want {
				t.Fatalf("AckBatch() exec count = %d, want %d", got, want)
			}
			if test.wantCapable {
				assertContainerImageIdentityAttemptBoundAckQuery(t, db.execs[0].query)
				if !strings.Contains(
					db.execs[0].query,
					"work_item_id = ANY(",
				) {
					t.Fatalf(
						"target batch ACK does not use grouped id arrays:\n%s",
						db.execs[0].query,
					)
				}
				if strings.Contains(db.execs[0].query, "unnest(") {
					t.Fatalf(
						"target batch ACK retained slower pair unnest:\n%s",
						db.execs[0].query,
					)
				}
				if got := len(db.execs[0].args); got < 4 || got%2 != 0 {
					t.Fatalf("target batch ACK arg count = %d, want grouped pairs", got)
				}
				gotPairs := make(map[string]int64, len(test.intents))
				for argIndex := 2; argIndex < len(db.execs[0].args); argIndex += 2 {
					ids, ok := db.execs[0].args[argIndex].([]string)
					if !ok {
						t.Fatalf(
							"target batch ACK ids arg %d = %#v, want []string",
							argIndex,
							db.execs[0].args[argIndex],
						)
					}
					epoch, ok := db.execs[0].args[argIndex+1].(int64)
					if !ok {
						t.Fatalf(
							"target batch ACK epoch arg %d = %#v, want int64",
							argIndex+1,
							db.execs[0].args[argIndex+1],
						)
					}
					for _, id := range ids {
						gotPairs[id] = epoch
					}
				}
				for _, intent := range test.intents {
					if intent.Domain != reducer.DomainContainerImageIdentity {
						continue
					}
					if gotPairs[intent.IntentID] != intent.ClaimEpoch {
						t.Fatalf(
							"target batch ACK epoch[%s] = %d, want %d",
							intent.IntentID,
							gotPairs[intent.IntentID],
							intent.ClaimEpoch,
						)
					}
				}
				if test.wantMixed {
					assertContainerImageIdentityLegacyAckQuery(t, db.execs[1].query)
					if got, want := db.execs[1].args[2], "intent-5854-batch-mixed-unrelated"; got != want {
						t.Fatalf("mixed unrelated ACK id = %#v, want %#v", got, want)
					}
				}
				return
			}
			assertContainerImageIdentityLegacyAckQuery(t, db.execs[0].query)
			if !strings.Contains(db.execs[0].query, "work_item_id IN (") {
				t.Fatalf(
					"legacy batch ACK no longer uses exact expanded predicate:\n%s",
					db.execs[0].query,
				)
			}
		})
	}
}

func assertContainerImageIdentityAttemptBoundAckQuery(t *testing.T, query string) {
	t.Helper()

	for _, want := range []string{
		"container_image_identity_v2_authorized_status",
		"THEN 'succeeded'",
		"container_image_identity_claim_epoch",
	} {
		if !strings.Contains(query, want) {
			t.Fatalf("reducer ACK query missing %q:\n%s", want, query)
		}
	}
	for _, forbidden := range []string{
		"set_config(",
		"eshu_internal.container_image_identity_ack_v1",
		"WITH ack_capability",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("target ACK query retained %q:\n%s", forbidden, query)
		}
	}
}

func assertContainerImageIdentityLegacyAckQuery(t *testing.T, query string) {
	t.Helper()
	for _, forbidden := range []string{
		"set_config(",
		"eshu_internal.container_image_identity_ack_v1",
		"WITH ack_capability",
	} {
		if strings.Contains(query, forbidden) {
			t.Fatalf("legacy ACK query contains %q:\n%s", forbidden, query)
		}
	}
}
