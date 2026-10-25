// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package queue

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

// Blockage captures eligible work that could not be claimed because a
// durable coordination gate is protecting the same conflict domain.
type Blockage struct {
	Stage          string
	Domain         string
	ConflictDomain string
	ConflictKey    string
	// HolderDomain is the reducer domain of the live claimed or running row
	// that holds the conflict fence. It is empty when no lease holds the
	// fence, as for a row gated by a missing readiness requirement.
	HolderDomain string
	Blocked      int
	OldestAge    time.Duration
}

// CloneBlockages normalizes queue-blockage rows into the same priority
// order used by the operator report: biggest and oldest blockers first.
func CloneBlockages(rows []Blockage) []Blockage {
	cloned := make([]Blockage, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Stage) == "" {
			continue
		}
		row.OldestAge = shared.NonNegativeDuration(row.OldestAge)
		cloned = append(cloned, row)
	}
	sort.Slice(cloned, func(i, j int) bool {
		if cloned[i].Blocked != cloned[j].Blocked {
			return cloned[i].Blocked > cloned[j].Blocked
		}
		if cloned[i].OldestAge != cloned[j].OldestAge {
			return cloned[i].OldestAge > cloned[j].OldestAge
		}
		if cloned[i].Stage != cloned[j].Stage {
			return cloned[i].Stage < cloned[j].Stage
		}
		if cloned[i].Domain != cloned[j].Domain {
			return cloned[i].Domain < cloned[j].Domain
		}
		return cloned[i].ConflictKey < cloned[j].ConflictKey
	})
	return cloned
}

// RenderBlockageLines formats conflict-blocked queue diagnostics for the
// text status report without promoting high-cardinality keys into metrics.
func RenderBlockageLines(rows []Blockage) []string {
	if len(rows) == 0 {
		return nil
	}

	lines := []string{"Blocked queue work:"}
	for _, row := range rows {
		holder := ""
		if row.HolderDomain != "" {
			holder = " holder_domain=" + row.HolderDomain
		}
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s domain=%s conflict_domain=%s conflict_key=%s%s blocked=%d oldest=%s",
				row.Stage,
				row.Domain,
				row.ConflictDomain,
				row.ConflictKey,
				holder,
				row.Blocked,
				row.OldestAge,
			),
		)
	}
	return lines
}
