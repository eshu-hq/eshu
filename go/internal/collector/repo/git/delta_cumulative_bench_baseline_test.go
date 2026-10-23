// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import "github.com/eshu-hq/eshu/go/internal/scope"

// chainBenchBaseline is the chained scope's baseline: the active delta c1 and
// the newest activated full F (#8780).
func chainBenchBaseline(full, active string) scope.DeltaSyncBaseline {
	return scope.DeltaSyncBaseline{ActiveCommitSHA: active, ActiveIsDelta: true, NewestActivatedFullCommitSHA: full}
}
