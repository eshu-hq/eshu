// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package secretsiam

import "context"

// GraphProjectionFollowUpWriter enqueues the secrets/IAM graph-projection
// follow-up intent after the trust-chain stage commits a generation's
// read-model facts. The projection handler loads those read models, so the
// intent must exist only once they are committed: nothing else creates it,
// and a generation whose follow-up is missing drains clean while its edges
// never materialize. A nil writer skips the enqueue, which preserves the
// flag-off behavior where DomainSecretsIAMGraphProjection is unregistered
// and a follow-up row would sit unclaimed.
type GraphProjectionFollowUpWriter interface {
	EnqueueGraphProjectionFollowUp(
		ctx context.Context,
		scopeID string,
		generationID string,
		sourceSystem string,
	) error
}
