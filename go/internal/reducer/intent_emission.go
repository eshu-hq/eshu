// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
	"github.com/eshu-hq/eshu/go/internal/reducer/sharedintent"
)

// ProjectionContext holds the bounded-unit freshness context for one shared
// projection repository slice. Alias for [sharedintent.ProjectionContext]: the
// shape and its acceptance-unit fallback live in that leaf so a domain family
// can build a context without importing this package.
type ProjectionContext = sharedintent.ProjectionContext

// copyPayload forwards to [payloadcore.CopyPayload].
func copyPayload(m map[string]any) map[string]any {
	return payloadcore.CopyPayload(m)
}
