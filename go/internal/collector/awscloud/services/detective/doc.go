// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package detective maps Amazon Detective metadata into AWS cloud collector
// facts.
//
// The package owns scanner-level fact selection for behavior graphs and their
// member accounts. It emits reported evidence only: behavior graph identity and
// creation time, and member-account identity and membership status. It keeps
// investigations, finding groups, indicators, usage volume, and member contact
// emails outside the scanner contract.
//
// It emits two relationships. The graph-to-member-account edge targets the AWS
// Organizations account node by bare account id, so graph membership joins
// org context. The graph-to-GuardDuty-detector edge targets the GuardDuty
// detector node by bare detector id and is emitted only when a real detector id
// is resolvable for the graph; Detective's metadata APIs do not report one, so
// the edge is omitted rather than dangled when no id is available.
package detective
