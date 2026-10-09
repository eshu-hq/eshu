// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package testfixtures

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// FactChannel returns a closed channel holding envelopes, the fact stream
// shape CommitScopeGeneration reads. It merges the identical
// testFactChannel twins from proof_domain_test.go and
// activation/fixtures_test.go (#7648).
func FactChannel(envelopes []facts.Envelope) <-chan facts.Envelope {
	ch := make(chan facts.Envelope, len(envelopes))
	for _, e := range envelopes {
		ch <- e
	}
	close(ch)
	return ch
}
