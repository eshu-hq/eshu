// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package golang holds the Go call resolver (package name golang, not go,
// because go is a keyword — the same precedent as internal/parser/golang):
// package-qualified import binding, method-return-chain inference, same-
// directory resolution, and cross-repo package-export resolution. It reads
// the shared entity index and import/path helpers from
// code/call/shared and never imports code/call/golang's siblings or the
// code/call dispatcher.
package golang
