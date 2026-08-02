// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestContainerImageIdentityV3CapabilityLatchCoversQueueLifecycle(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"single claim": claimReducerWorkQuery,
		"batch claim":  claimReducerWorkBatchQuery,
		"ack":          ackContainerImageIdentityReducerWorkQuery,
		"retry":        retryContainerImageIdentityReducerWorkQuery,
		"fail":         failContainerImageIdentityReducerWorkQuery,
		"reopen":       reopenSucceededReducerWorkQuery,
		"replay":       replaySucceededReducerDomainQuery,
		"recovery":     replayFailedWorkItemsTemplate,
		"poison":       recoverPoisonDeadLettersQuery,
		"supersede":    supersedeInactiveReducerGenerationsCTE,
	}
	for name, query := range queries {
		if !strings.Contains(query, "container_image_identity_v3_authorized_status") {
			t.Errorf("%s query does not advance digest-v3 authorization", name)
		}
	}
	for name, query := range map[string]string{
		"single claim": claimReducerWorkQuery,
		"batch claim":  claimReducerWorkBatchQuery,
	} {
		if !strings.Contains(query, "container_image_identity_v3_required") {
			t.Errorf("%s query does not advertise digest-v3 capability", name)
		}
	}
}
