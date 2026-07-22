// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "github.com/eshu-hq/eshu/go/internal/parser"

// effectiveSnapshotParseWorkers reports the actual parser worker cardinality
// when the zero-value configuration falls back to the sequential parser path.
func effectiveSnapshotParseWorkers(configured int) int {
	if configured <= 1 {
		return 1
	}
	return configured
}

func (s NativeRepositorySnapshotter) engine() (*parser.Engine, error) {
	if s.Engine != nil {
		return s.Engine, nil
	}
	return parser.DefaultEngine()
}

func (s NativeRepositorySnapshotter) registry() parser.Registry {
	if len(s.Registry.ParserKeys()) > 0 {
		return s.Registry
	}
	return parser.DefaultRegistry()
}
