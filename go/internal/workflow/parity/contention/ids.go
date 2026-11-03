// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"fmt"
	"strings"
)

// Prefix is the single synthetic id prefix the run writes under
// (REMOVAL_CONTENTION_SPEC.md §4). Every run, work item, scope, claim, and
// owner id the harness creates starts with it, and every cleanup and residue
// query filters on it. The value holds no LIKE wildcard ('%' or '_'), so the
// pattern Prefix+"%" matches the literal prefix only.
const Prefix = "synth-8875-rehearsal-"

// The harness id families. Census, residue, and every detector filter on
// the family that matches the column, not on the bare Prefix: the deployed
// synthetic instances use scope ids such as synth-8875-rehearsal-tfstate,
// which carry the bare prefix but are not harness rows.
const (
	runFamily      = Prefix + "run-%"
	itemFamily     = Prefix + "wi-%"
	scopeFamily    = Prefix + "scope-%"
	instanceFamily = Prefix + "instance-%"
	claimFamily    = Prefix + "claim-%"
)

// seedRunPattern and seedItemPattern match only one seed's rows: cleanup
// deletes those and never another run's.
func seedRunPattern(seed int64) string  { return fmt.Sprintf("%srun-%d-%%", Prefix, seed) }
func seedItemPattern(seed int64) string { return fmt.Sprintf("%swi-%d-%%", Prefix, seed) }

// InstanceID is the synthetic collector instance every harness item and
// selector names. Claim selection filters on (kind, instance), so no real
// collector can claim a harness item and no harness worker can claim a real
// one.
const InstanceID = Prefix + "instance-1"

// IsSynthetic reports whether id carries the synthetic prefix.
func IsSynthetic(id string) bool {
	return strings.HasPrefix(id, Prefix)
}

// ids builds the deterministic §4 identifiers for one seed, so a rerun of
// the same seed produces the same rows.
type ids struct {
	seed int64
}

func (i ids) run(n int) string      { return fmt.Sprintf("%srun-%d-%d", Prefix, i.seed, n) }
func (i ids) workItem(n int) string { return fmt.Sprintf("%swi-%d-%d", Prefix, i.seed, n) }
func (i ids) scope(k int) string    { return fmt.Sprintf("%sscope-%d", Prefix, k) }
func (i ids) owner(w int) string    { return fmt.Sprintf("%sowner-%d", Prefix, w) }
func (i ids) claim(w, n int) string { return fmt.Sprintf("%sclaim-%d-%d-%d", Prefix, i.seed, w, n) }
func (i ids) advisoryRun(r, g int) string {
	return fmt.Sprintf("%srun-%d-adv-%d-%d", Prefix, i.seed, r, g)
}

func (i ids) advisoryItem(r, g, n int) string {
	return fmt.Sprintf("%swi-%d-adv-%d-%d-%d", Prefix, i.seed, r, g, n)
}

// advisoryTarget is the shared acceptance unit of one advisory target.
func (i ids) advisoryTarget(r, n int) string {
	return fmt.Sprintf("%sau-adv-%d-%d-%d", Prefix, i.seed, r, n)
}

// advisoryScope keeps advisory rounds on scopes no backlog item uses, so the
// guard sees no open target from the backlog.
func (i ids) advisoryScope(r, n int) string {
	return fmt.Sprintf("%sscope-adv-%d-%d", Prefix, r, n)
}

// quotaScope names the two quota-workload scopes: "at" is held at budget,
// "below" stays under it.
func (i ids) quotaScope(name string) string { return fmt.Sprintf("%sscope-quota-%s", Prefix, name) }
