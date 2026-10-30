// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package cabal reads the tested-with field of Haskell .cabal descriptions.
// Only a column-zero field is read; anything else fails closed. Every entry
// passes the shared SafeVersionSpec guard, so credentials drop counted.
package cabal
