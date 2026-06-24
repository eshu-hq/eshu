// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestReducerConflictDomainKeyClassifiesResourceMaterializationDomains(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		domain     reducer.Domain
		entityKey  string
		wantStatus reducerResourceConflictStatus
		wantDomain string
		wantPrefix string
	}{
		{
			name:       "aws resource nodes use hashed versioned resource-node key",
			domain:     reducer.DomainAWSResourceMaterialization,
			entityKey:  "aws_resource_materialization:aws:111122223333:us-east-1:ec2",
			wantStatus: reducerResourceConflictStatusSafe,
			wantDomain: reducerConflictDomainCloudResourceNode,
			wantPrefix: reducerConflictKeyPrefixCloudResourceNode,
		},
		{
			name:       "gcp resource nodes remain whole-scope until partitioned handlers exist",
			domain:     reducer.DomainGCPResourceMaterialization,
			entityKey:  "gcp_resource_materialization:gcp:project:demo:global",
			wantStatus: reducerResourceConflictStatusRisky,
			wantDomain: reducerConflictDomainResourceScope,
			wantPrefix: reducerConflictKeyPrefixResourceScope,
		},
		{
			name:       "azure relationship edges remain blocked whole-scope",
			domain:     reducer.DomainAzureRelationshipMaterialization,
			entityKey:  "azure_resource_materialization:azure:subscription:demo",
			wantStatus: reducerResourceConflictStatusBlocked,
			wantDomain: reducerConflictDomainResourceScope,
			wantPrefix: reducerConflictKeyPrefixResourceScope,
		},
		{
			name:       "iam permission graph writes remain blocked whole-scope",
			domain:     reducer.DomainIAMCanPerformMaterialization,
			entityKey:  "aws_resource_materialization:aws:111122223333:global:iam",
			wantStatus: reducerResourceConflictStatusBlocked,
			wantDomain: reducerConflictDomainResourceScope,
			wantPrefix: reducerConflictKeyPrefixResourceScope,
		},
		{
			name:       "security group reachability remains blocked whole-scope",
			domain:     reducer.DomainSecurityGroupReachabilityMaterialization,
			entityKey:  "aws_resource_materialization:aws:111122223333:us-east-1:ec2",
			wantStatus: reducerResourceConflictStatusBlocked,
			wantDomain: reducerConflictDomainResourceScope,
			wantPrefix: reducerConflictKeyPrefixResourceScope,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy, ok := reducerResourceConflictPolicyFor(tt.domain)
			if !ok {
				t.Fatalf("reducerResourceConflictPolicyFor(%q) ok = false, want true", tt.domain)
			}
			if policy.Status != tt.wantStatus {
				t.Fatalf("policy status = %q, want %q", policy.Status, tt.wantStatus)
			}

			gotDomain, gotKey := reducerConflictDomainKey(projector.ReducerIntent{
				ScopeID:   "aws:111122223333:us-east-1:ec2",
				Domain:    tt.domain,
				EntityKey: tt.entityKey,
			})
			if gotDomain != tt.wantDomain {
				t.Fatalf("conflict domain = %q, want %q", gotDomain, tt.wantDomain)
			}
			if !strings.HasPrefix(gotKey, tt.wantPrefix) {
				t.Fatalf("conflict key = %q, want prefix %q", gotKey, tt.wantPrefix)
			}
			for _, leaked := range []string{"111122223333", "us-east-1", "ec2"} {
				if strings.Contains(gotKey, leaked) {
					t.Fatalf("conflict key %q leaks raw provider value %q", gotKey, leaked)
				}
			}
		})
	}
}

func TestReducerConflictDomainKeyRejectsRawProviderLocators(t *testing.T) {
	t.Parallel()

	rawValues := []string{
		strings.Join([]string{"arn", "aws", "iam", "", "111122223333", "role/Admin"}, ":"),
		"/sub" + "scriptions/00000000/resourceGroups/prod/providers/Microsoft.Compute/virtualMachines/app",
		"//cloudresourcemanager.googleapis.com/projects/demo",
		"/private/repos/team/service",
		"https://metadata" + ".example.internal/latest",
		"cred" + "ential:" + "secret" + "-token",
		"scope-with-" + strings.Join([]string{"10", "0", "0", "4"}, "."),
	}
	for _, raw := range rawValues {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			gotDomain, gotKey := reducerConflictDomainKey(projector.ReducerIntent{
				ScopeID:   raw,
				Domain:    reducer.DomainAWSResourceMaterialization,
				EntityKey: raw,
			})
			if gotDomain != reducerConflictDomainResourceScope {
				t.Fatalf("conflict domain = %q, want safe fallback %q", gotDomain, reducerConflictDomainResourceScope)
			}
			if !strings.HasPrefix(gotKey, reducerConflictKeyPrefixResourceScope) {
				t.Fatalf("conflict key = %q, want fallback prefix %q", gotKey, reducerConflictKeyPrefixResourceScope)
			}
			if strings.Contains(gotKey, raw) {
				t.Fatalf("conflict key %q copied raw provider locator %q", gotKey, raw)
			}
		})
	}
}

func TestReducerResourceConflictPolicyCoversIssue2754Domains(t *testing.T) {
	t.Parallel()

	required := []reducer.Domain{
		reducer.DomainAWSResourceMaterialization,
		reducer.DomainGCPResourceMaterialization,
		reducer.DomainAzureResourceMaterialization,
		reducer.DomainAWSRelationshipMaterialization,
		reducer.DomainGCPRelationshipMaterialization,
		reducer.DomainAzureRelationshipMaterialization,
		reducer.DomainIAMCanAssumeMaterialization,
		reducer.DomainIAMEscalationMaterialization,
		reducer.DomainIAMCanPerformMaterialization,
		reducer.DomainS3LogsToMaterialization,
		reducer.DomainS3ExternalPrincipalGrantMaterialization,
		reducer.DomainS3InternetExposureMaterialization,
		reducer.DomainRDSPostureMaterialization,
		reducer.DomainEC2InstanceNodeMaterialization,
		reducer.DomainEC2UsesProfileMaterialization,
		reducer.DomainEC2InternetExposureMaterialization,
		reducer.DomainEC2BlockDeviceKMSPostureMaterialization,
		reducer.DomainKubernetesWorkloadMaterialization,
		reducer.DomainKubernetesCorrelationMaterialization,
		reducer.DomainSecurityGroupCidrMaterialization,
		reducer.DomainSecurityGroupRuleMaterialization,
		reducer.DomainSecurityGroupReachabilityMaterialization,
	}

	statuses := map[reducerResourceConflictStatus]bool{}
	for _, domain := range required {
		policy, ok := reducerResourceConflictPolicyFor(domain)
		if !ok {
			t.Fatalf("missing resource conflict policy for %q", domain)
		}
		if strings.TrimSpace(policy.Evidence) == "" {
			t.Fatalf("policy for %q has blank evidence", domain)
		}
		statuses[policy.Status] = true
	}
	for _, status := range []reducerResourceConflictStatus{
		reducerResourceConflictStatusSafe,
		reducerResourceConflictStatusRisky,
		reducerResourceConflictStatusBlocked,
	} {
		if !statuses[status] {
			t.Fatalf("issue #2754 policy coverage missing status %q", status)
		}
	}
}
