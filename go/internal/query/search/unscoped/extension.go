// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unscoped

import "time"

// phaseExtension is the density-gated continuation past phase one (#8002):
// it runs only when the last ordered window predicts the page fills inside
// the shape's extension row allowance.
const phaseExtension phase = "extension"

// extensionAllowance reports how long the density-gated extension may walk
// past phase one, or zero when the tail runs next. It extends only when the
// last completed ordered window holds matches and predicts the page fills
// inside the shape's extension row allowance, at last-window density rounded
// up. The time allowance walks at most that allowance worth of rows and
// always leaves the tail floor, so a wrong prediction still reaches today's
// tail path. A zero figure disables the extension: the file route keeps it
// at zero and its walk is byte-identical.
func (w *walk[R, K]) extensionAllowance() time.Duration {
	maxRows := w.plan.extensionRows
	if maxRows <= 0 || w.finished || len(w.matches) >= w.want {
		return 0
	}
	if w.lastWindowRows <= 0 || w.lastWindowMatches <= 0 || w.lastWindowCost <= 0 {
		return 0
	}
	short := w.want - len(w.matches)
	needRows := (short*w.lastWindowRows + w.lastWindowMatches - 1) / w.lastWindowMatches
	if needRows <= 0 || needRows > maxRows {
		return 0
	}
	capTime := time.Duration(int64(maxRows) * int64(w.lastWindowCost) / int64(w.lastWindowRows))
	if room := w.remaining() - w.plan.tailFloor; room < capTime {
		if room <= 0 {
			return 0
		}
		return room
	}
	return capTime
}
