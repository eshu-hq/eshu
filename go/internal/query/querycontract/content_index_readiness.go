// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"errors"
	"net/http"
)

// ErrContentSubstringIndexesNotReady means an all-repository substring read
// was refused until the exact content trigram indexes finish finalizing.
//
// This moved here from root package query's content_reader_index_readiness.go
// (#6060) so a future handler-family subpackage can compare a returned error
// against this exact value with errors.Is, the same as root's ContentReader
// that produces it. The topic-investigation and structural-inventory routes
// that will need it still live in root today; root keeps a plain var alias, so
// nothing depends on that split landing.
var ErrContentSubstringIndexesNotReady = errors.New("content substring indexes are not ready")

// K8sSelectCandidateScanTruncationReason is the machine-readable disclosure
// reason emitted on the entity-context API/MCP response when a k8s SELECTS
// relationship build's K8sResource candidate scan hits
// RepositorySemanticEntityLimit and had to be truncated. It is emitted only
// when truncation actually occurs so every repo under the limit gets
// byte-identical responses. The implementation moved from root's
// content_relationships.go for #6060 so the entity-handler family subpackage
// can disclose it without importing root.
const K8sSelectCandidateScanTruncationReason = "k8s_resource_candidate_scan_truncated_at_5000"

// WriteContentSubstringIndexUnavailable writes the stable 503 contract for
// ErrContentSubstringIndexesNotReady and reports whether it did. It returns
// false without touching the response when err is not that error.
func WriteContentSubstringIndexUnavailable(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, ErrContentSubstringIndexesNotReady) {
		return false
	}
	WriteError(w, http.StatusServiceUnavailable, err.Error())
	return true
}
