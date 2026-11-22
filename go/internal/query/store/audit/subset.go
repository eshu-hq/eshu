// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package audit

import (
	"fmt"
	"slices"
	"sort"
)

// Violation is one read a capability did not declare: an undeclared store with
// the statement that read it, or an unresolved read the classifier could not
// attribute.
type Violation struct {
	// Store is the undeclared token, or "unresolved" for an unattributed read.
	Store string
	// Evidence names the statement or the reason.
	Evidence string
}

// String renders the violation for a test failure.
func (v Violation) String() string {
	return fmt.Sprintf("%s (%s)", v.Store, v.Evidence)
}

// Undeclared returns every store in used that declared does not contain, plus
// every unresolved read, in a stable order. An empty result means the used set
// is a subset of the declaration.
func Undeclared(declared []string, used Used) []Violation {
	var out []Violation
	stores := make([]string, 0, len(used.Stores))
	for store := range used.Stores {
		stores = append(stores, store)
	}
	sort.Strings(stores)
	for _, store := range stores {
		if !slices.Contains(declared, store) {
			out = append(out, Violation{Store: store, Evidence: used.Stores[store]})
		}
	}
	for _, reason := range used.Unresolved {
		out = append(out, Violation{Store: "unresolved", Evidence: reason})
	}
	return out
}
