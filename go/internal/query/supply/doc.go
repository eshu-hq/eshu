// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package supply is a path namespace, not a package with behavior. It exists
// because "supplychain" is a glued compound (docs/internal/naming.md rule 3)
// and splits into nested directories rather than staying glued. Its single
// child, chain/, is the supply-chain query hub; see chain/doc.go and
// chain/README.md for the family it groups (issue #6642).
package supply
