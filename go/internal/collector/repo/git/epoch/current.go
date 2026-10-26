// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package epoch

import (
	_ "embed"
	"fmt"
	"sync"
)

// epochLength is the number of lowercase hex characters in an epoch.
const epochLength = 16

//go:embed ledger.tsv
var embeddedLedger []byte

// current parses the embedded ledger once. The ledger is committed and
// embedded at build time, so a malformed one is a build defect that every test
// touching the freshness hint would hit; it panics rather than fold a wrong
// value into every hint.
var current = sync.OnceValue(func() string {
	return mustCurrent(embeddedLedger)
})

// mustCurrent returns the epoch folded into every freshness hint. It panics
// on a malformed, unshaped, or unsealed ledger rather than fold a wrong epoch:
// Parse accepts a hand-merged ledger (it does not check the seal) and the
// epoch shape survives a merge, so only an explicit Sealed check fails the
// merge closed here instead of shipping new output under an old hint.
func mustCurrent(data []byte) string {
	ledger, err := Parse(data)
	if err != nil {
		panic(fmt.Sprintf("git content epoch: parse embedded %s: %v", LedgerFile, err))
	}
	if !ValidEpoch(ledger.Epoch) {
		panic(fmt.Sprintf("git content epoch: embedded %s epoch %q is not %d lowercase hex characters", LedgerFile, ledger.Epoch, epochLength))
	}
	if !ledger.Sealed() {
		panic(fmt.Sprintf("git content epoch: embedded %s is not sealed: re-pin, then resolve conflicts with origin/main before re-pinning", LedgerFile))
	}
	return ledger.Epoch
}

// Current returns the content epoch of the ledger embedded in this build. It
// is deterministic across builds of the same tree, so an unchanged ledger
// keeps every freshness hint.
func Current() string {
	return current()
}

// ValidEpoch reports whether value is a chained epoch: exactly 16 lowercase
// hex characters.
func ValidEpoch(value string) bool {
	if len(value) != epochLength {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
