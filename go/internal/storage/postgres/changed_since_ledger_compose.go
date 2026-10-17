// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"database/sql"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/status/changedsince"
)

// ledgerLink is one walked link of a ledger-read chain, in chain order.
type ledgerLink struct {
	generationID string
	priorID      string
	kind         string
	digest       int64
	deltaRows    int64
	liveKeys     map[changedsince.Category]int
}

// ledgerDelta is one stored link-delta row of a ledger-read chain.
type ledgerDelta struct {
	category       string
	classification string
	key            string
	priorKind      sql.NullString
	currentKind    sql.NullString
	priorState     []byte
	currentState   []byte
	tombstoned     bool
}

// ledgerComposedEntry is one key of the composed end-to-end diff, before
// unchanged counts and samples are derived from the live totals and the
// state table. Unchanged keys need no entry: they are everything live that
// is not composed-changed.
type ledgerComposedEntry struct {
	category       changedsince.Category
	classification changedsince.Classification
	key            string
	kind           string
}

// ledgerFactCategory maps a fact_kind to its evidence category, mirroring
// the CASE in changedSinceClassificationCTEs and the link writer's cur CTE.
func ledgerFactCategory(kind string) changedsince.Category {
	switch kind {
	case "file":
		return changedsince.CategoryFiles
	case "content_entity":
		return changedsince.CategoryContentEntities
	default:
		return changedsince.CategoryFacts
	}
}

// ledgerTouch is the earliest and latest link touching one key. Only the
// endpoints matter: the legacy diff compares the prior generation against
// the current one, so intermediate touches cancel out.
type ledgerTouch struct {
	firstPriorState  []byte
	firstPriorKind   string
	firstPriorCat    changedsince.Category
	lastCurrentState []byte
	lastCurrentKind  string
	lastCurrentCat   changedsince.Category
	categoryChanged  bool
}

// composeLedgerTouches groups ordered per-link deltas by key. Dropped rows
// (a tombstone of a never-live key) carry no state either way, so they only
// matter as a touch when the key has no other touch on that link; grouping
// by key across links handles that without special-casing.
func composeLedgerTouches(deltasByLink [][]ledgerDelta) map[string]*ledgerTouch {
	touches := map[string]*ledgerTouch{}
	for _, deltas := range deltasByLink {
		for _, delta := range deltas {
			touch, ok := touches[delta.key]
			if !ok {
				touch = &ledgerTouch{
					firstPriorState: delta.priorState,
					firstPriorKind:  firstString(delta.priorKind.String, delta.currentKind.String),
					firstPriorCat:   changedsince.Category(delta.category),
				}
				touches[delta.key] = touch
			}
			touch.lastCurrentState = delta.currentState
			if delta.currentKind.Valid {
				touch.lastCurrentKind = delta.currentKind.String
				touch.lastCurrentCat = ledgerFactCategory(delta.currentKind.String)
			} else if delta.priorKind.Valid {
				touch.lastCurrentKind = ""
				touch.lastCurrentCat = ""
			}
			if touch.firstPriorCat != "" && touch.lastCurrentCat != "" &&
				touch.firstPriorCat != touch.lastCurrentCat {
				touch.categoryChanged = true
			}
		}
	}
	return touches
}

func firstString(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// composeLedgerEntries reduces touches to end-to-end entries plus the keys
// whose end is absent and whose start was present: those need the final
// generation's tombstone probe to tell retired from superseded, mirroring
// the legacy tombstone join. Keys absent at both ends are invisible to the
// legacy diff (a tombstone of a never-live key lands in its NULL-category
// bucket, which the assembler drops), so they compose to nothing.
func composeLedgerEntries(touches map[string]*ledgerTouch) ([]ledgerComposedEntry, []string) {
	var entries []ledgerComposedEntry
	var removed []string
	for key, touch := range touches {
		startPresent := touch.firstPriorState != nil
		endPresent := touch.lastCurrentState != nil
		switch {
		case !startPresent && !endPresent:
			continue
		case !startPresent && endPresent:
			entries = append(entries, ledgerComposedEntry{
				category:       touch.lastCurrentCat,
				classification: changedsince.Added,
				key:            key,
				kind:           touch.lastCurrentKind,
			})
		case startPresent && endPresent && touch.categoryChanged:
			entries = append(entries,
				ledgerComposedEntry{
					category:       touch.firstPriorCat,
					classification: changedsince.Superseded,
					key:            key,
					kind:           touch.firstPriorKind,
				},
				ledgerComposedEntry{
					category:       touch.lastCurrentCat,
					classification: changedsince.Added,
					key:            key,
					kind:           touch.lastCurrentKind,
				})
		case startPresent && endPresent && !bytes.Equal(touch.firstPriorState, touch.lastCurrentState):
			entries = append(entries, ledgerComposedEntry{
				category:       touch.lastCurrentCat,
				classification: changedsince.Updated,
				key:            key,
				kind:           touch.lastCurrentKind,
			})
		case startPresent && !endPresent:
			removed = append(removed, key)
		}
	}
	sort.Strings(removed)
	return entries, removed
}

// classifyLedgerRemoved resolves removed keys against the final
// generation's tombstone probe (key to its tombstone fact_kinds). A key with
// a same-category tombstone still carried by the final generation is
// retired, with the least tombstone kind, mirroring the legacy MIN(fact_kind)
// tombstone aggregate; without one it is superseded with its prior kind.
func classifyLedgerRemoved(
	touches map[string]*ledgerTouch,
	removed []string,
	tombstones map[string][]string,
) []ledgerComposedEntry {
	var entries []ledgerComposedEntry
	for _, key := range removed {
		touch := touches[key]
		kind := ""
		for _, tombKind := range tombstones[key] {
			if ledgerFactCategory(tombKind) == touch.firstPriorCat && (kind == "" || tombKind < kind) {
				kind = tombKind
			}
		}
		if kind != "" {
			entries = append(entries, ledgerComposedEntry{
				category:       touch.firstPriorCat,
				classification: changedsince.Retired,
				key:            key,
				kind:           kind,
			})
			continue
		}
		entries = append(entries, ledgerComposedEntry{
			category:       touch.firstPriorCat,
			classification: changedsince.Superseded,
			key:            key,
			kind:           touch.firstPriorKind,
		})
	}
	return entries
}

// sortLedgerEntries orders composed entries the way the legacy lateral
// samples do: by category, classification, then key.
func sortLedgerEntries(entries []ledgerComposedEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].category != entries[j].category {
			return entries[i].category < entries[j].category
		}
		if entries[i].classification != entries[j].classification {
			return entries[i].classification < entries[j].classification
		}
		return entries[i].key < entries[j].key
	})
}
