// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
	"sort"
)

// SecretsIAMEndpointNotReadyFailureClass identifies a cross-scope endpoint
// readiness miss that should defer without consuming the reducer retry budget.
const SecretsIAMEndpointNotReadyFailureClass = "secrets_iam_endpoint_not_ready"

// checkEndpointReadiness gates the secrets/IAM graph projection on uid-exact
// cross-scope endpoint readiness (issue #1380). It collects the distinct
// KubernetesWorkload and CloudResource endpoint uids the extracted edges
// reference and confirms each is committed via the presence lookup. If any are
// missing it returns a retryable not-ready error so the durable queue re-runs the
// intent once the endpoints commit, instead of writing a projection that would
// silently drop those edges. A nil PresenceLookup disables gating (the writer
// no-ops a missing endpoint anyway); an empty endpoint set is trivially ready.
func (h SecretsIAMGraphProjectionHandler) checkEndpointReadiness(
	ctx context.Context, intent Intent, rows SecretsIAMGraphRows,
) error {
	if h.PresenceLookup == nil {
		return nil
	}

	workloadUIDs := distinctEdgeEndpointUIDs(rows.UsesServiceAccountEdges, "workload_uid")
	cloudResourceUIDs := distinctEdgeEndpointUIDs(rows.AssumesIAMRoleEdges, "cloud_resource_uid")

	for _, check := range []struct {
		keyspace GraphProjectionKeyspace
		uids     []string
	}{
		{GraphProjectionKeyspaceKubernetesWorkloadUID, workloadUIDs},
		{GraphProjectionKeyspaceCloudResourceUID, cloudResourceUIDs},
	} {
		if len(check.uids) == 0 {
			continue
		}
		missing, err := h.PresenceLookup.MissingUIDs(ctx, check.keyspace, check.uids)
		if err != nil {
			return fmt.Errorf("look up %s endpoint presence: %w", check.keyspace, err)
		}
		if len(missing) > 0 {
			return secretsIAMEndpointsNotReadyError{
				scopeID:      intent.ScopeID,
				generationID: intent.GenerationID,
				keyspace:     string(check.keyspace),
				missingCount: len(missing),
			}
		}
	}
	return nil
}

// distinctEdgeEndpointUIDs returns the sorted distinct non-empty values of key
// across the edge rows. Sorting keeps the lookup bound and any error message
// deterministic.
func distinctEdgeEndpointUIDs(edgeRows []map[string]any, key string) []string {
	if len(edgeRows) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(edgeRows))
	for _, row := range edgeRows {
		uid, _ := row[key].(string)
		if uid == "" {
			continue
		}
		seen[uid] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	uids := make([]string, 0, len(seen))
	for uid := range seen {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	return uids
}

// secretsIAMEndpointsNotReadyError marks a cross-scope endpoint-readiness miss as
// retryable so the durable queue re-runs the projection once the endpoint nodes
// commit, instead of failing terminally or writing edges to absent endpoints. It
// never names a specific uid (which could be a redacted identifier); only the
// bounded keyspace and a count.
type secretsIAMEndpointsNotReadyError struct {
	scopeID      string
	generationID string
	keyspace     string
	missingCount int
}

func (e secretsIAMEndpointsNotReadyError) Error() string {
	return fmt.Sprintf(
		"%d %s endpoint node(s) not committed for secrets/iam projection scope %s generation %s",
		e.missingCount, e.keyspace, e.scopeID, e.generationID,
	)
}

func (secretsIAMEndpointsNotReadyError) Retryable() bool { return true }

func (secretsIAMEndpointsNotReadyError) FailureClass() string {
	return SecretsIAMEndpointNotReadyFailureClass
}
