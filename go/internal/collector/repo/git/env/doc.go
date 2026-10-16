// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package env holds the environment coercion helpers the git collector's
// config loader shares. It is a leaf: package git imports it, and it must
// never import package git. It depends only on the standard library, so it
// stays importable without pulling the collector in.
package env
