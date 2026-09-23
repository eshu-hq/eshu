// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestScopeMetadataLearnsInSortedOrder (round 3 F7): two metadata values
// whose IPv4 pseudonyms collide must get the same pseudonyms on every run.
// Payload keys are already visited sorted; scope metadata was iterated in
// map order, which made the probed pseudonym depend on iteration order.
func TestScopeMetadataLearnsInSortedOrder(t *testing.T) {
	key, err := NewKey([]byte("6965-p3-order-key-0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	slotOf := func(raw string) int { return int(binary.BigEndian.Uint64(key.mac(raw)) % ipv4Slots) }
	first, second := "", ""
	seen := map[int]string{}
	for i := 1; i < 10000 && second == ""; i++ {
		raw := fmt.Sprintf("10.%d.%d.%d", i/65536, i/256%256, i%256)
		if owner, ok := seen[slotOf(raw)]; ok {
			first, second = owner, raw
		}
		seen[slotOf(raw)] = raw
	}
	if second == "" {
		t.Fatal("no colliding address pair found")
	}
	policy := Policy{Fields: map[string]Class{"private_ipv4_address": ClassIPv4, "public_ip_address": ClassIPv4}}
	run := func() string {
		observedAt := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
		sc := scope.IngestionScope{
			ScopeID: "s", SourceSystem: "aws", ScopeKind: scope.KindRegion, CollectorKind: scope.CollectorAWS, PartitionKey: "s",
			Metadata: map[string]string{"private_ipv4_address": first, "public_ip_address": second},
		}
		g := scope.ScopeGeneration{GenerationID: "gen-1", ScopeID: "s", ObservedAt: observedAt, IngestedAt: observedAt, Status: scope.GenerationStatusPending, TriggerKind: scope.TriggerKindSnapshot}
		src := &orderSource{gen: collector.FactsFromSlice(sc, g, nil)}
		wrapped, report, err := Wrap(src, key, policy)
		if err != nil {
			t.Fatal(err)
		}
		gen, ok, err := wrapped.Next(context.Background())
		if err != nil || !ok {
			t.Fatalf("Next: ok=%v err=%v", ok, err)
		}
		if report.IPv4Collisions != 1 {
			t.Fatalf("collisions = %d, want exactly 1 (the pair must collide)", report.IPv4Collisions)
		}
		return gen.Scope.Metadata["private_ipv4_address"] + "|" + gen.Scope.Metadata["public_ip_address"]
	}
	want := run()
	for i := 0; i < 40; i++ {
		if got := run(); got != want {
			t.Fatalf("run %d: metadata pseudonyms depend on iteration order", i)
		}
	}
}

type orderSource struct {
	gen  collector.CollectedGeneration
	done bool
}

func (s *orderSource) Next(context.Context) (collector.CollectedGeneration, bool, error) {
	if s.done {
		return collector.CollectedGeneration{}, false, nil
	}
	s.done = true
	return s.gen, true, nil
}
