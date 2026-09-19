// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package inventory

// ForRepositories restricts a Filter to the given repositories so live reader
// tests stay exact in a shared database. Production readers never restrict by
// repository in this package: scoped-token reads stay on the graph because
// two infra labels are authorized through edges a repo-keyed table cannot
// express.
func (f Filter) ForRepositories(repoIDs ...string) Filter {
	f.repoIDs = append([]string(nil), repoIDs...)
	return f
}
