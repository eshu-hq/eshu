// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"fmt"
)

// ContainerImageExistenceLookup reports which candidate target ContainerImage
// node uids are already materialized in the canonical graph. It exists so
// AWSCloudImageMaterializationHandler.Handle can distinguish "this
// lambda_function_uses_image reference parsed to a well-formed exact digest"
// (which ExtractAWSCloudImageEdgeRows alone can determine, having no graph
// access) from "the graph actually has that :ContainerImage node" (which only
// a live read can determine) — issue #5450 P1 follow-up: without this check,
// a digest whose OCI registry was never scanned would still be counted as a
// materialized edge in the metric, CanonicalWrites, and evidence summary,
// even though the writer's two-MATCH-MERGE silently no-ops on the missing
// target.
type ContainerImageExistenceLookup interface {
	// ExistingContainerImageUIDs returns the subset of uids that currently
	// exist as :ContainerImage nodes. Empty uids are ignored. A nil/empty
	// result with a nil error means none of the candidates exist.
	ExistingContainerImageUIDs(ctx context.Context, uids []string) (map[string]struct{}, error)
}

// GraphContainerImageExistenceLookup implements ContainerImageExistenceLookup
// against the canonical graph through the same generic GraphQueryRunner
// GraphInfrastructurePlatformLookup uses.
type GraphContainerImageExistenceLookup struct {
	Graph GraphQueryRunner
}

// containerImageExistingUIDsCypher is a single-clause UNWIND read: the
// UNWIND-bound candidate value anchors a concrete MATCH, and the RETURN alias
// uses a distinct name from the UNWIND binding variable, per the NornicDB
// variable-shadowing pitfall (docs/public/reference/nornicdb-pitfalls.md).
// Reads are not subject to the pinned backend's bare-MATCH SET no-op (issue
// #5652); this query only reads, so it needs no MERGE workaround.
const containerImageExistingUIDsCypher = `UNWIND $candidate_uids AS candidate_uid
MATCH (image:ContainerImage {uid: candidate_uid})
RETURN image.uid AS existing_uid`

// ExistingContainerImageUIDs runs containerImageExistingUIDsCypher over the
// given candidate uids and returns the confirmed-existing subset as a set.
func (l GraphContainerImageExistenceLookup) ExistingContainerImageUIDs(
	ctx context.Context,
	uids []string,
) (map[string]struct{}, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	if l.Graph == nil {
		return nil, fmt.Errorf("graph container image existence lookup requires graph query runner")
	}

	candidates := make([]any, 0, len(uids))
	for _, uid := range uids {
		if uid != "" {
			candidates = append(candidates, uid)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	rows, err := l.Graph.Run(ctx, containerImageExistingUIDsCypher, map[string]any{
		"candidate_uids": candidates,
	})
	if err != nil {
		return nil, fmt.Errorf("read existing container image uids: %w", err)
	}

	existing := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if uid, ok := row["existing_uid"].(string); ok && uid != "" {
			existing[uid] = struct{}{}
		}
	}
	return existing, nil
}
