// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ec2instance

import (
	"context"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/facts"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// EC2InstanceNodeRetracter deletes globally-dead EC2 instance node uids
// under the owner-ledger gate (graphowner.EC2InstanceRetracter in
// production). Like the shared cloud retracter it MUST be idempotent and
// MUST only delete uids the global posture-tuple live-check proved dead in
// every scope. A nil retracter is a no-op so the domain stays safe to
// register before the retract slice is wired.
type EC2InstanceNodeRetracter interface {
	RetractDeadEC2InstanceNodes(
		ctx context.Context,
		candidates []reducercontract.EC2PostureCandidate,
		evidenceSource string,
	) (retracted int, err error)
}

// retractDeadEC2InstanceNodes runs one EC2 generation-diff retract: it
// resolves the scope's predecessor generation, extracts that generation's
// posture rows, and hands the predecessor-only rows to the globally-gated
// retracter as posture candidates in sorted uid order. The semantics mirror
// the shared cloud helper: nil seams skip, no predecessor or an empty diff
// issues no call, and any other failure returns an error so the durable
// queue retries the whole intent.
func retractDeadEC2InstanceNodes(
	ctx context.Context,
	loader factload.FactLoader,
	priorGeneration reducercontract.PriorGenerationID,
	retracter EC2InstanceNodeRetracter,
	scopeID string,
	generationID string,
	currentRows []map[string]any,
	evidenceSource string,
) (int, error) {
	if retracter == nil || priorGeneration == nil {
		return 0, nil
	}
	prior, found, err := priorGeneration(ctx, scopeID, generationID)
	if err != nil {
		return 0, fmt.Errorf("resolve prior generation for ec2 instance retract: %w", err)
	}
	if !found {
		return 0, nil
	}
	priorEnvelopes, err := factload.LoadFactsForKinds(
		ctx, loader, scopeID, prior, []string{facts.EC2InstancePostureFactKind},
	)
	if err != nil {
		return 0, fmt.Errorf("load prior generation posture facts for ec2 instance retract: %w", err)
	}
	priorRows, _, _, err := ExtractEC2InstanceNodeRowsWithSkips(priorEnvelopes)
	if err != nil {
		return 0, fmt.Errorf("extract prior generation uids for ec2 instance retract: %w", err)
	}
	current := make(map[string]struct{}, len(currentRows))
	for _, row := range currentRows {
		if uid, _ := row["uid"].(string); uid != "" {
			current[uid] = struct{}{}
		}
	}
	candidates := make([]reducercontract.EC2PostureCandidate, 0, len(priorRows))
	for _, row := range priorRows {
		uid, _ := row["uid"].(string)
		if uid == "" {
			continue
		}
		if _, ok := current[uid]; ok {
			continue
		}
		candidates = append(candidates, ec2PostureCandidateFromRow(uid, row))
	}
	if len(candidates) == 0 {
		return 0, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].UID < candidates[j].UID })
	retracted, err := retracter.RetractDeadEC2InstanceNodes(ctx, candidates, evidenceSource)
	if err != nil {
		return 0, fmt.Errorf("retract dead ec2 instance nodes: %w", err)
	}
	return retracted, nil
}

// ec2PostureCandidateFromRow projects one extracted node row back into the
// posture identity tuple the liveness probe matches. InstanceID carries the
// row's merged resource_id and ARN the row's arn: the probe resolves the
// reference as instance-or-arn with the reader's COALESCE semantics, which is
// exact for all three row shapes (instance+arn, arn-only with
// resource_id=arn, instance-only with blank arn).
func ec2PostureCandidateFromRow(uid string, row map[string]any) reducercontract.EC2PostureCandidate {
	return reducercontract.EC2PostureCandidate{
		UID:          uid,
		AccountID:    payloadcore.AnyToString(row["account_id"]),
		Region:       payloadcore.AnyToString(row["region"]),
		ResourceType: payloadcore.AnyToString(row["resource_type"]),
		InstanceID:   payloadcore.AnyToString(row["resource_id"]),
		ARN:          payloadcore.AnyToString(row["arn"]),
	}
}
