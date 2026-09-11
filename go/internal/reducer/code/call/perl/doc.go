// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package perl holds the Perl call resolver: package-import-bound callee
// resolution for `Package::function` calls. It reads the shared entity index
// from code/call/shared and never imports code/call or its sibling language
// leaves.
package perl
