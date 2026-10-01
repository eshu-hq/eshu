// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"sync/atomic"
)

// sequentialImpactFencingTokenIssuer issues strictly increasing tokens from 1,
// like the production Postgres sequence, and records how many it issued.
type sequentialImpactFencingTokenIssuer struct {
	next   atomic.Int64
	issued atomic.Int64
}

func (i *sequentialImpactFencingTokenIssuer) NextSupplyChainImpactFencingToken(context.Context) (int64, error) {
	i.issued.Add(1)
	return i.next.Add(1), nil
}

// newTestImpactFencingTokenIssuer returns an issuer for handler tests that do
// not care about the token value, only that the handler can be fenced.
func newTestImpactFencingTokenIssuer() *sequentialImpactFencingTokenIssuer {
	return &sequentialImpactFencingTokenIssuer{}
}
