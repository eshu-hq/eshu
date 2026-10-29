// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type publicAdvisoryPolicyStoreStub struct {
	requests []workflow.PublicAdvisoryPolicyReconcile
	onCall   func()
	err      error
}

func (s *publicAdvisoryPolicyStoreStub) ReconcilePublicAdvisoryRegistrations(
	_ context.Context, request workflow.PublicAdvisoryPolicyReconcile,
) error {
	if s.onCall != nil {
		s.onCall()
	}
	s.requests = append(s.requests, request)
	return s.err
}

func TestPublicAdvisoryPolicyReconcilesBeforeGenericInstances(t *testing.T) {
	t.Parallel()
	ordinary := &fakeStore{}
	first := true
	protected := &publicAdvisoryPolicyStoreStub{onCall: func() {
		if first && len(ordinary.observed) != 0 {
			t.Fatal("generic reconcile ran before protected policy")
		}
		first = false
	}}
	instance := workflow.DesiredCollectorInstance{
		InstanceID: "advisory-owned", CollectorKind: scope.CollectorVulnerabilityIntelligence,
		Configuration: `{"targets":[],"public_advisory_targets":[]}`,
	}
	service := Service{
		Config: Config{
			CollectorInstances: []workflow.DesiredCollectorInstance{instance},
			PublicAdvisoryPolicy: PublicAdvisoryPolicy{
				ExpectedRevision: 0, DesiredRevision: 1,
				InstanceIDs: []string{"advisory-owned"}, Explicit: true,
			},
		},
		Store: ordinary, PublicAdvisoryPolicyStore: protected,
		ControlRolePolicyRevision: 7, ControlDatabaseIdentity: "db-id",
	}
	if err := service.runReconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(protected.requests) != 1 || len(ordinary.observed) != 1 ||
		len(protected.requests[0].Instances) != 1 ||
		protected.requests[0].Instances[0].InstanceID != "advisory-owned" {
		t.Fatalf("protected=%+v generic calls=%d", protected.requests, len(ordinary.observed))
	}
	protected.err = errors.New("policy stale")
	if err := service.runReconcile(context.Background()); err == nil {
		t.Fatal("stale policy accepted")
	}
	if len(ordinary.observed) != 1 {
		t.Fatal("generic reconcile ran after protected policy failure")
	}
}

func TestPublicAdvisoryPolicyRequiresAttestedControlBeforeGenericReconcile(t *testing.T) {
	t.Parallel()
	ordinary := &fakeStore{}
	protected := &publicAdvisoryPolicyStoreStub{}
	service := Service{
		Config: Config{PublicAdvisoryPolicy: PublicAdvisoryPolicy{
			Explicit: true, DesiredRevision: 1,
		}},
		Store: ordinary, PublicAdvisoryPolicyStore: protected,
	}
	if err := service.runReconcile(context.Background()); err == nil ||
		len(protected.requests) != 0 || len(ordinary.observed) != 0 {
		t.Fatalf("unattested policy error=%v protected=%d ordinary=%d",
			err, len(protected.requests), len(ordinary.observed))
	}
}
