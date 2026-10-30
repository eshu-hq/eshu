// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	"github.com/eshu-hq/eshu/go/internal/reducer/ssmhost"
	ssmwriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/cloud/aws/ssm"
)

const (
	// ssmHostMaxCaptureAgeEnv bounds the age of an inventory capture that
	// may supersede the prior generation (eshu-hq/eshu#9558). Required:
	// unset or invalid leaves the SSM host domain unwired.
	ssmHostMaxCaptureAgeEnv = "ESHU_SSM_HOST_MAX_CAPTURE_AGE"
	// ssmHostMaxCrossTypeSkewEnv bounds the allowed capture-time difference
	// between the platform and application evidence admitted as one host
	// view. Required alongside the capture age.
	ssmHostMaxCrossTypeSkewEnv = "ESHU_SSM_HOST_MAX_CROSS_TYPE_SKEW"
	// ssmHostMaxPlatformObservationAgeEnv bounds the age of a
	// DescribeInstanceInformation platform observation, which carries no
	// inventory capture time. Required alongside the capture age.
	ssmHostMaxPlatformObservationAgeEnv = "ESHU_SSM_HOST_MAX_PLATFORM_OBSERVATION_AGE"
)

// loadSSMHostBounds reads the fail-closed SSM host freshness configuration.
// Every bound must parse as a positive duration; a missing or invalid value
// zeroes the bounds so Bounds.Validate fails and the registry leaves
// DomainSSMHostInventoryMaterialization unwired rather than projecting under
// unbounded rules.
func loadSSMHostBounds(getenv func(string) string) ssmhost.Bounds {
	if getenv == nil {
		return ssmhost.Bounds{}
	}
	return ssmhost.Bounds{
		MaxCaptureAge:             strictPositiveDuration(getenv(ssmHostMaxCaptureAgeEnv)),
		MaxCrossTypeSkew:          strictPositiveDuration(getenv(ssmHostMaxCrossTypeSkewEnv)),
		MaxPlatformObservationAge: strictPositiveDuration(getenv(ssmHostMaxPlatformObservationAgeEnv)),
	}
}

func strictPositiveDuration(raw string) time.Duration {
	value, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return 0
	}
	return value
}

// ssmHostGraphWriterAdapter adapts the cypher SSM host writer onto the
// reducer's HostGraphWriter interface, translating host tuples and committed
// captures between the storage and reducer shapes. The cmd layer owns this
// translation so the cypher leaf never imports the reducer.
type ssmHostGraphWriterAdapter struct {
	writer *ssmwriter.HostNodeWriter
}

func (a ssmHostGraphWriterAdapter) ReadCommittedHosts(
	ctx context.Context,
	scopeID string,
	hosts []cloud.SSMHostTuple,
) (map[string]ssmhost.CommittedHost, error) {
	storageHosts := make([]ssmwriter.HostTuple, 0, len(hosts))
	for _, host := range hosts {
		storageHosts = append(storageHosts, ssmwriter.HostTuple{
			Partition:  host.Partition,
			AccountID:  host.AccountID,
			Region:     host.Region,
			InstanceID: host.InstanceID,
		})
	}
	committed, err := a.writer.ReadCommittedHosts(ctx, scopeID, storageHosts)
	if err != nil {
		return nil, err
	}
	out := make(map[string]ssmhost.CommittedHost, len(committed))
	for _, host := range hosts {
		storage, ok := committed[ssmwriter.HostKey(ssmwriter.HostTuple{
			Partition:  host.Partition,
			AccountID:  host.AccountID,
			Region:     host.Region,
			InstanceID: host.InstanceID,
		})]
		if !ok {
			continue
		}
		out[ssmhost.HostKey(host)] = ssmhost.CommittedHost{
			AppCapture:    storage.AppCapture,
			HasAppCapture: storage.HasAppCapture,
		}
	}
	return out, nil
}

func (a ssmHostGraphWriterAdapter) WriteHostNodes(ctx context.Context, write ssmhost.HostGraphWrite) error {
	return a.writer.WriteHostObservationBatch(
		ctx,
		write.Apps,
		write.Platforms,
		write.Edges,
		write.ScopeID,
		write.GenerationID,
		write.EvidenceSource,
	)
}

func (a ssmHostGraphWriterAdapter) RetractStaleHostNodes(ctx context.Context, retract ssmhost.HostGraphRetract) (int, error) {
	total := 0
	if retract.HasAppCapture {
		stale, err := a.writer.RetractStaleApplicationNodes(
			ctx,
			retract.ScopeID,
			retract.EvidenceSource,
			ssmwriter.HostTuple{
				Partition:  retract.Host.Partition,
				AccountID:  retract.Host.AccountID,
				Region:     retract.Host.Region,
				InstanceID: retract.Host.InstanceID,
			},
			retract.AppCaptureOlderThan,
		)
		if err != nil {
			return total, err
		}
		total += stale
	}
	cleared, err := a.writer.RetractPlatformNodes(ctx, retract.ScopeID, retract.EvidenceSource, retract.DeletePlatformUIDs)
	if err != nil {
		return total, err
	}
	return total + cleared, nil
}
