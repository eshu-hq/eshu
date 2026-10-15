// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package gitlab lists one GitLab group's projects for the git collector's
// gitlabGroup source mode (#7765).
//
// It is a leaf: package git imports it, and it must never import package
// git. Shared helpers come from collector/sdk (which imports no Eshu
// package) and operator signals from internal/telemetry. Selection,
// identity, and observation stay in package git and its membership leaf.
package gitlab
