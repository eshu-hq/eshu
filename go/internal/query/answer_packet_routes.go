// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// serviceStoryAnswerData attaches the service.story answer packet companion
// to a service-story response body. The implementation moved to
// querycontract for #6060; this wrapper keeps root callers unchanged.
func serviceStoryAnswerData(serviceName string, data map[string]any, truth *TruthEnvelope) map[string]any {
	return querycontract.ServiceStoryAnswerData(serviceName, data, truth)
}
