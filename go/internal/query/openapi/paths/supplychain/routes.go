// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package supplychain

// Routes concatenates `impactFindings`, `impactExplain`, `suppressionMutation`
// in the order the published document expects. It holds no JSON of its own;
// changing the order here reorders the assembled spec.
const Routes = impactFindings +
	impactExplain +
	suppressionMutation
