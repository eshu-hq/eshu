// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eol

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// embedded carries the vendored endoflife.date per-product files plus the
// snapshot_date of their fetch, so the read path resolves EOL status from
// compiled-in data and never touches the network.
//
//go:embed snapshots/*.json snapshots/snapshot_date
var embedded embed.FS

// dateLayout is the snapshot_date and endoflife.date day format.
const dateLayout = "2006-01-02"

// RefreshWindowDays is the number of days a snapshot stays fresh. An answer
// whose snapshot is older than this reports Stale, signalling the refresh
// job has not landed a new snapshot in its window.
const RefreshWindowDays = 30

// cycle is one normalized endoflife.date product cycle. EOL is either false
// (rolling release, still maintained) or a YYYY-MM-DD day; Support holds the
// YYYY-MM-DD end of active support and stays empty when the product does not
// publish one.
type cycle struct {
	// Cycle is the endoflife.date cycle name, for example "20" or "1.24".
	Cycle string `json:"cycle"`
	// EOLFalse is true when the product reports "eol": false.
	EOLFalse bool `json:"eol_maintained"`
	// EOL is the end-of-life day, empty when EOLFalse or unreported.
	EOL string `json:"eol,omitempty"`
	// Support is the end-of-active-support day, empty when unreported.
	Support string `json:"support,omitempty"`
}

// Snapshot is one vendored endoflife.date fetch: the UTC day it was taken
// and the normalized cycles per mapped slug.
type Snapshot struct {
	// Date is the UTC day of the fetch, YYYY-MM-DD.
	Date string `json:"snapshot_date"`
	// Products maps an endoflife.date slug to its vendored cycles.
	Products map[string][]cycle `json:"products"`
}

// rawCycle mirrors the wire shape whose eol, support, extendedSupport and
// lts fields vary per product: eol and support are each a day, false, or
// absent. Only cycle, eol and support feed the join.
type rawCycle struct {
	Cycle   string          `json:"cycle"`
	EOL     json.RawMessage `json:"eol"`
	Support json.RawMessage `json:"support"`
}

// dayOrEmpty decodes a day-or-false-or-absent wire value to its day, or to
// empty when it is false, absent, or not a day string.
func dayOrEmpty(raw json.RawMessage) string {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "false" {
		return ""
	}
	var day string
	if err := json.Unmarshal(raw, &day); err != nil {
		return ""
	}
	if _, err := time.Parse(dateLayout, day); err != nil {
		return ""
	}
	return day
}

// normalizeCycle keeps the fields the join needs and drops the per-product
// drift (extendedSupport, lts, pep, link). A missing eol key leaves both the
// date and the rolling flag unset, so the join reads it as unknown.
func normalizeCycle(raw rawCycle) cycle {
	out := cycle{Cycle: raw.Cycle, Support: dayOrEmpty(raw.Support)}
	if len(raw.EOL) != 0 && strings.TrimSpace(string(raw.EOL)) == "false" {
		out.EOLFalse = true
		return out
	}
	out.EOL = dayOrEmpty(raw.EOL)
	return out
}

// ParseSnapshot normalizes one fetch: files maps an endoflife.date slug to
// its per-product JSON body, and date is the UTC day of the fetch. It fails
// on a malformed date or product body rather than vendoring data the join
// would misread.
func ParseSnapshot(files map[string][]byte, date string) (Snapshot, error) {
	if _, err := time.Parse(dateLayout, date); err != nil {
		return Snapshot{}, fmt.Errorf("eol: bad snapshot_date %q: %w", date, err)
	}
	sn := Snapshot{Date: date, Products: make(map[string][]cycle, len(files))}
	for slug, body := range files {
		var raw []rawCycle
		if err := json.Unmarshal(body, &raw); err != nil {
			return Snapshot{}, fmt.Errorf("eol: bad product JSON for %q: %w", slug, err)
		}
		cycles := make([]cycle, 0, len(raw))
		for _, r := range raw {
			cycles = append(cycles, normalizeCycle(r))
		}
		sn.Products[slug] = cycles
	}
	return sn, nil
}

// LoadSnapshot parses the snapshot vendored into the binary. The read path
// calls this once and reuses the Snapshot; no call in this package performs
// network I/O, which is what keeps air-gapped deployments identical to
// connected ones modulo staleness.
func LoadSnapshot() (Snapshot, error) {
	entries, err := embedded.ReadDir("snapshots")
	if err != nil {
		return Snapshot{}, fmt.Errorf("eol: read embedded snapshot: %w", err)
	}
	files := make(map[string][]byte)
	var date string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		body, err := embedded.ReadFile("snapshots/" + entry.Name())
		if err != nil {
			return Snapshot{}, fmt.Errorf("eol: read embedded %q: %w", entry.Name(), err)
		}
		if entry.Name() == "snapshot_date" {
			date = strings.TrimSpace(string(body))
			continue
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			files[strings.TrimSuffix(entry.Name(), ".json")] = body
		}
	}
	return ParseSnapshot(files, date)
}
