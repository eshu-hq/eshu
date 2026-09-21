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

// Cloud-identity and Terraform-state fact kinds seeded into fact_records so
// the read-API routes that serve from them are exercised at corpus scale.
// GET /api/v0/cloud/inventory reads reducer_cloud_resource_identity
// (query.cloudInventoryFactKind); without these rows it reads a handful of
// blocks and its work budget collapses to the default row, which is no guard
// at all.
//
// Payload keys are the ones the collectors and the reducer emit. Admission
// identities join provider facts per provider identity key (AWS arn, GCP
// full_resource_name, Azure arm_resource_id); EC2 posture rows carry their
// own identity; state resources join provider bindings per resource address.
// The corpus mirrors nodesPerLabel 1:1.
//
// #6843 introduced this seed to feed a Postgres read model for the
// graph-only labels. #6912 removed that read model, and these routes no
// longer read fact_records -- but /cloud/inventory always did, so the seed
// stays and keeps its budget meaningful.
const (
	cloudIdentityFactKind = "reducer_cloud_resource_identity"
	ec2PostureFactKind    = "ec2_instance_posture"
	stateResourceFactKind = "terraform_state_resource"
	stateBindingFactKind  = "terraform_state_provider_binding"
)

// SeedCloudStateFact is one fact_records row for the cloud/state corpus.
type SeedCloudStateFact struct {
	FactID       string
	ScopeID      string
	GenerationID string
	Kind         string
	SourceSystem string
	Payload      map[string]any
}

// BuildCloudStateFacts renders count cloud/state facts anchored on one
// scope generation: one admission identity plus its provider fact per i,
// one EC2 posture fact per AWS i, and one state resource plus its provider
// binding per i. Providers cycle aws/gcp/azure so the provider rollup has
// real buckets, not an all-one degenerate case.
func BuildCloudStateFacts(scopeID, generationID string, count int) []SeedCloudStateFact {
	facts := make([]SeedCloudStateFact, 0, count*4+count/3)
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
			SeedCloudStateFact{
				FactID:  fmt.Sprintf("%s-graphonly-adm-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: cloudIdentityFactKind, SourceSystem: provider,
				Payload: map[string]any{
					"cloud_resource_uid": uid,
					"resource_type":      resourceType,
					"raw_identity":       rawIdentity,
					"provider":           provider,
				},
			},
			SeedCloudStateFact{
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
			facts = append(facts, SeedCloudStateFact{
				FactID:  fmt.Sprintf("%s-graphonly-ec2-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: ec2PostureFactKind, SourceSystem: "aws",
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
			SeedCloudStateFact{
				FactID:  fmt.Sprintf("%s-graphonly-tsr-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: stateResourceFactKind, SourceSystem: "tfstate",
				Payload: map[string]any{
					"address": address,
					"type":    "aws_instance",
					"name":    fmt.Sprintf("gate-%d", i),
				},
			},
			SeedCloudStateFact{
				FactID:  fmt.Sprintf("%s-graphonly-bind-%d", generationID, i),
				ScopeID: scopeID, GenerationID: generationID,
				Kind: stateBindingFactKind, SourceSystem: "tfstate",
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

// SeedCloudStateFacts bulk-inserts the cloud/state corpus into fact_records
// via COPY, reusing the IaC fact column shape.
func SeedCloudStateFacts(ctx context.Context, pool *pgxpool.Pool, facts []SeedCloudStateFact, now time.Time) error {
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
		return fmt.Errorf("seed cloud/state fact_records: %w", err)
	}
	return nil
}
