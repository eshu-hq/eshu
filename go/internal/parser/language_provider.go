// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

// LanguageProvider owns parse and optional pre-scan behavior for one registered
// language. Implementations must be deterministic and safe for concurrent calls
// because the engine may run pre-scan providers from a worker pool.
type LanguageProvider interface {
	Parse(ParseRequest) (map[string]any, error)
	PreScan(PreScanRequest) ([]string, error)
	Capabilities() LanguageCapabilities
}

// ParseRequest carries the normalized engine inputs for a provider parse.
type ParseRequest struct {
	RepoRoot     string
	Path         string
	IsDependency bool
	Options      Options
}

// PreScanRequest carries the normalized engine inputs for a provider pre-scan.
type PreScanRequest struct {
	RepoRoot string
	Path     string
}

// LanguageCapabilities describes optional language-owned semantic depth.
type LanguageCapabilities struct {
	TreeSitter bool
	SCIP       bool
	PreScan    bool
}
