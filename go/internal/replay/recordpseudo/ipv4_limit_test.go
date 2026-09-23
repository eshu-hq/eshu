// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/recordpolicy"
	"github.com/eshu-hq/eshu/go/internal/replay/recordpseudo"
)

// TestIPv4CeilingIsAnError (P1): the 763rd distinct IPv4 address in one
// recording is a returned error that names the limit, never a panic, and
// 762 distinct addresses still record.
func TestIPv4CeilingIsAnError(t *testing.T) {
	key := mustKey(t, keyA)
	addresses := func(n int) []map[string]any {
		out := make([]map[string]any, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, map[string]any{"private_ipv4_address": fmt.Sprintf("10.0.%d.%d", i/256, i%256)})
		}
		return out
	}
	_, envs, report := wrapGens(t, &sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:ec2", nil, addresses(762)...),
	}}, key, recordpolicy.Policy())
	if len(envs[0]) != 762 || report.IPv4Addresses != 762 {
		t.Fatalf("762 addresses: facts=%d report.IPv4Addresses=%d", len(envs[0]), report.IPv4Addresses)
	}
	wrapped, _, err := recordpseudo.Wrap(&sliceSource{gens: []collector.CollectedGeneration{
		generation("aws:"+acct+":us-east-1:ec2", nil, addresses(763)...),
	}}, key, recordpolicy.Policy())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = wrapped.Next(context.Background())
	if err == nil {
		t.Fatal("763 distinct addresses: Next returned no error")
	}
	if !errors.Is(err, recordpseudo.ErrIPv4Exhausted) || !strings.Contains(err.Error(), "exceeds 762 distinct IPv4 addresses") {
		t.Errorf("error = %v, want ErrIPv4Exhausted naming the 762 limit", err)
	}
	// The failure is sticky (round 3 F3): a caller that polls again must see
	// the same error, never a clean end of batch.
	_, ok, err := wrapped.Next(context.Background())
	if ok || !errors.Is(err, recordpseudo.ErrIPv4Exhausted) {
		t.Errorf("second Next after exhaustion: ok=%v err=%v, want the same error", ok, err)
	}
}
