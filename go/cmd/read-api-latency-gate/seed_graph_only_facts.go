// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Graph-only fact kinds the #6843 Reader serves from fact_records, with the
// payload keys the collectors and the reducer emit. Admission identities
// join provider facts per provider identity key (AWS arn, GCP
// full_resource_name, Azure arm_resource_id); EC2 posture rows carry their
// own identity; state resources join provider bindings per resource
// address. The corpus mirrors nodesPerLabel 1:1 so the cold read measures
// the same scale the whole-label graph scan used to walk.
const (
	graphOnlyAdmissionKind = "reducer_cloud_resource_identity"
	graphOnlyEC2Kind       = "ec2_instance_posture"
	graphOnlyStateKind     = "terraform_state_resource"
	graphOnlyBindingKind   = "terraform_state_provider_binding"
)

// SeedGraphOnlyFact is one fact_records row for the graph-only corpus.
type SeedGraphOnlyFact struct {
	FactID       string
	ScopeID      string
	GenerationID string
	Kind         string
	SourceSystem string
	Payload      map[string]any
}

// BuildGraphOnlyFacts renders count graph-only facts anchored on one
// scope generation: one admission identity plus its provider fact per i,
// one EC2 posture fact per AWS i, and one state resource plus its provider
// binding per i. Providers cycle aws/gcp/azure so the provider rollup has
// real buckets, not an all-one degenerate case.
func BuildGraphOnlyFacts(scopeID, generationID string, count int) []SeedGraphOnlyFact {
	facts := make([]SeedGraphOnlyFact, 0, count*4+count/3)
	for i := 0; i < count; i++ {
		provider := seedProviders[i%len(seedProviders)]
		uid := fmt.Sprintf("gate-cr-%d", i)
		var rawIdentity, providerKind, identityKey, identityValue, serviceKind, resourceType string
		switch provider {
		case "gcp":
			resourceType = "gce_instance"
			rawIdentity = fmt.Sprintf("//compute.googleapis.com/projects/p/zones/z/instances/gate-%d", i)
			providerKind, identityKey, identityValue = "gcp_cloud_resource", "full_resource_name", rawIdentity
			serviceKind = "gce"
		case "azure":
			resourceType = "azure_vm"
			rawIdentity = fmt.Sprintf("/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/gate-%d", i)
			providerKind, identityKey, identityValue = "azure_cloud_resource", "arm_resource_id", rawIdentity
			serviceKind = "compute"
		default:
			provider = "aws"
			resourceType = "aws_instance"
			rawIdentity = fmt.Sprintf("arn:aws:ec2:us-east-1:111:instance/gate-%d", i)
			providerKind, identityKey, identityValue = "aws_resource", "arn", rawIdentity
			serviceKind = "ec2"
		}
		facts = append(facts,
			SeedGraphOnlyFact{
				FactID:  fmt.Sprintf("%s-graphonly-adm-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: graphOnlyAdmissionKind, SourceSystem: provider,
				Payload: map[string]any{
					"cloud_resource_uid": uid,
					"resource_type":      resourceType,
					"raw_identity":       rawIdentity,
					"provider":           provider,
				},
			},
			SeedGraphOnlyFact{
				FactID:  fmt.Sprintf("%s-graphonly-prov-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: providerKind, SourceSystem: provider,
				Payload: map[string]any{
					identityKey:    identityValue,
					"service_kind": serviceKind,
				},
			},
		)
		if provider == "aws" {
			facts = append(facts, SeedGraphOnlyFact{
				FactID:  fmt.Sprintf("%s-graphonly-ec2-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: graphOnlyEC2Kind, SourceSystem: "aws",
				Payload: map[string]any{
					"account_id":   "111",
					"region":       "us-east-1",
					"instance_id":  fmt.Sprintf("gate-i-%d", i),
					"service_kind": "ec2",
				},
			})
		}
		address := fmt.Sprintf("aws_instance.gate%06d", i)
		facts = append(facts,
			SeedGraphOnlyFact{
				FactID:  fmt.Sprintf("%s-graphonly-tsr-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: graphOnlyStateKind, SourceSystem: "tfstate",
				Payload: map[string]any{
					"address": address,
					"type":    "aws_instance",
					"name":    fmt.Sprintf("gate-%d", i),
				},
			},
			SeedGraphOnlyFact{
				FactID:  fmt.Sprintf("%s-graphonly-bind-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: graphOnlyBindingKind, SourceSystem: "tfstate",
				Payload: map[string]any{
					"resource_address": address,
					"provider_address": "registry.terraform.io/hashicorp/aws",
					"provider_type":    "aws",
				},
			},
		)
	}
	return facts
}

// SeedGraphOnlyFacts bulk-inserts the graph-only corpus into fact_records
// via COPY, reusing the IaC fact column shape.
func SeedGraphOnlyFacts(ctx context.Context, pool *pgxpool.Pool, facts []SeedGraphOnlyFact, now time.Time) error {
	rows := make([][]any, 0, len(facts))
	for _, f := range facts {
		payload, err := json.Marshal(f.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload for fact %s: %w", f.FactID, err)
		}
		rows = append(rows, []any{
			f.FactID, f.ScopeID, f.GenerationID, f.Kind, f.FactID,
			f.SourceSystem, f.FactID, now, now, payload,
		})
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"fact_records"},
		iacFactColumns,
		pgx.CopyFromRows(rows),
	); err != nil {
		return fmt.Errorf("seed graph-only fact_records: %w", err)
	}
	return nil
}
