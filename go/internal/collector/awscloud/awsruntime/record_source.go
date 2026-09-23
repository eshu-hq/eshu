// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// recordLeaseTTL bounds the credential lease of one record-mode scan. It is
// only a ceiling for STS session duration; record mode holds no workflow
// claim to heartbeat.
const recordLeaseTTL = time.Hour

// RecordSource adapts a ClaimedSource to collector.Source for
// `-mode=record`: it synthesizes one work item per (account x allowed region
// x allowed service) from the runtime config, in config order, and resolves
// each through NextClaimed exactly as a workflow claim would -- same
// credential acquisition, same scanner registry, same envelope builders --
// but with no workflow row, no Postgres and no durable commit. Checkpoints,
// ScanStatus and Limiter may be nil; ClaimedSource tolerates all three.
type RecordSource struct {
	// Claimed is the live source wiring minus the Postgres-backed stores.
	Claimed ClaimedSource
	// Clock stamps the synthesized work items; nil means time.Now.
	Clock func() time.Time

	items []workflow.WorkItem
	index int
	built bool
}

// Next implements collector.Source: one generation per target tuple, then
// exhausted.
func (s *RecordSource) Next(ctx context.Context) (collector.CollectedGeneration, bool, error) {
	if !s.built {
		items, err := s.workItems()
		if err != nil {
			return collector.CollectedGeneration{}, false, err
		}
		s.items = items
		s.built = true
	}
	if s.index >= len(s.items) {
		return collector.CollectedGeneration{}, false, nil
	}
	item := s.items[s.index]
	s.index++
	generation, found, err := s.Claimed.NextClaimed(ctx, item)
	if err != nil {
		return collector.CollectedGeneration{}, false, fmt.Errorf("record scope %q: %w", item.ScopeID, err)
	}
	if !found {
		return s.Next(ctx)
	}
	return generation, true, nil
}

// workItems synthesizes the claims. Shape follows the scheduled planner
// (go/internal/coordinator/planner/aws/scheduled): scope id
// `aws:<account>:<region>:<service>`, acceptance unit the JSON claim target,
// fencing token 1, generation id equal to source run id.
func (s *RecordSource) workItems() ([]workflow.WorkItem, error) {
	instanceID := strings.TrimSpace(s.Claimed.Config.CollectorInstanceID)
	if instanceID == "" {
		return nil, fmt.Errorf("aws record source requires a collector instance id")
	}
	if len(s.Claimed.Config.Targets) == 0 {
		return nil, fmt.Errorf("aws record source requires at least one target scope")
	}
	now := time.Now().UTC()
	if s.Clock != nil {
		now = s.Clock().UTC()
	}
	var items []workflow.WorkItem
	for _, target := range s.Claimed.Config.Targets {
		account := strings.TrimSpace(target.AccountID)
		for _, region := range target.AllowedRegions {
			for _, service := range target.AllowedServices {
				item, err := recordWorkItem(instanceID, account, strings.TrimSpace(region), strings.TrimSpace(service), now)
				if err != nil {
					return nil, err
				}
				items = append(items, item)
			}
		}
	}
	return items, nil
}

func recordWorkItem(instanceID, account, region, service string, now time.Time) (workflow.WorkItem, error) {
	claim, err := json.Marshal(claimTarget{AccountID: account, Region: region, ServiceKind: service})
	if err != nil {
		return workflow.WorkItem{}, fmt.Errorf("encode AWS record claim target: %w", err)
	}
	scopeID := "aws:" + account + ":" + region + ":" + service
	generationID := "aws_record:" + scopeID
	item := workflow.WorkItem{
		WorkItemID:          fmt.Sprintf("%s:%s:record:%s", scope.CollectorAWS, instanceID, scopeID),
		RunID:               fmt.Sprintf("%s:%s:record", scope.CollectorAWS, instanceID),
		CollectorKind:       scope.CollectorAWS,
		CollectorInstanceID: instanceID,
		SourceSystem:        string(scope.CollectorAWS),
		ScopeID:             scopeID,
		AcceptanceUnitID:    string(claim),
		SourceRunID:         generationID,
		GenerationID:        generationID,
		FairnessKey:         fmt.Sprintf("%s:%s:%s", scope.CollectorAWS, instanceID, account),
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "record",
		CurrentFencingToken: 1,
		CurrentOwnerID:      instanceID,
		LeaseExpiresAt:      now.Add(recordLeaseTTL),
		VisibleAt:           now,
		LastClaimedAt:       now,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	return item, nil
}
