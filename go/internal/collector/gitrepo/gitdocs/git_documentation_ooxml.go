// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitdocs

import "github.com/eshu-hq/eshu/go/internal/collector/preflight/ooxml"

func ooxmlPreflightBlocksExtraction(result ooxml.Result) bool {
	for _, warning := range result.Warnings {
		switch warning.Class {
		case "", ooxml.WarningAnnotationTextSkipped, ooxml.WarningHiddenContentSkipped:
			continue
		default:
			return true
		}
	}
	return false
}
