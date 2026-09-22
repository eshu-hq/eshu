// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supply is a path namespace, not a package with behavior. It exists
// because "supplychain" is a glued compound (docs/internal/naming.md rule 3)
// and splits into nested directories rather than staying glued. Its single
// child, chain/, declares the supply-chain fact families; see chain/doc.go
// and chain/README.md for what it groups (issue #6776). The same split
// already names the query side at go/internal/query/supply/chain.
package supply
