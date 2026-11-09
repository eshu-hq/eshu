// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"
	"time"

	advisorygroup "github.com/eshu-hq/eshu/go/internal/reducer/advisory/group"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

// The #10411 registration seams. Both ports are nil-tolerant by design (a
// nil group-projection enqueuer silently skips the enqueue, and the
// advisory-group handler errors loudly only when an intent actually
// claims), so a dropped wire would keep every logic test green while the
// AdvisoryGroup graph family silently never projects. These tests
// observe the registered handlers carrying the exact ports passed in,
// mirroring TestSupplyChainImpactRegistrationCarriesTheFanOutSeam.

type registrationAdvisoryWinners struct{ reads int }

func (r *registrationAdvisoryWinners) QualifyingAdvisoryGroupWinners(context.Context, string) ([]advisorygroup.WinnerRow, error) {
	r.reads++
	return nil, nil
}

func (r *registrationAdvisoryWinners) AdvisoryGroupWinnersWatermark(context.Context) (time.Time, error) {
	return time.Time{}, nil
}

type registrationAdvisoryWriter struct{ reconciles int }

func (w *registrationAdvisoryWriter) ReconcileAdvisoryGroup(context.Context, advisorygroup.Group) (advisorygroup.WriteStats, error) {
	w.reconciles++
	return advisorygroup.WriteStats{}, nil
}

func TestAdvisoryGroupRegistrationCarriesTheProjectionPorts(t *testing.T) {
	t.Parallel()

	winners := &registrationAdvisoryWinners{}
	writer := &registrationAdvisoryWriter{}
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			AdvisoryGroupWinnersReader: winners,
			AdvisoryGroupWriter:        writer,
		},
	})
	for _, definition := range definitions {
		if definition.Domain != DomainAdvisoryGroupProjection {
			continue
		}
		handler, ok := definition.Handler.(advisorygroup.Handler)
		if !ok {
			t.Fatalf("handler for %s = %T, want advisorygroup.Handler", definition.Domain, definition.Handler)
		}
		if handler.Winners == nil || handler.Writer == nil {
			t.Fatal("a projection port is nil on the registered handler: every advisory_group_projection intent would fail")
		}
		if _, err := handler.Winners.QualifyingAdvisoryGroupWinners(context.Background(), "CVE-1"); err != nil {
			t.Fatalf("QualifyingAdvisoryGroupWinners() error = %v", err)
		}
		if _, err := handler.Writer.ReconcileAdvisoryGroup(context.Background(), advisorygroup.Group{ID: "CVE-1"}); err != nil {
			t.Fatalf("ReconcileAdvisoryGroup() error = %v", err)
		}
		if winners.reads != 1 || writer.reconciles != 1 {
			t.Fatalf("port calls = %d reads, %d reconciles, want 1/1: the registration wired different ports", winners.reads, writer.reconciles)
		}
		return
	}
	t.Fatalf("no %s registration found", DomainAdvisoryGroupProjection)
}

type registrationGroupProjectionEnqueuer struct{ calls int }

func (e *registrationGroupProjectionEnqueuer) EnqueueAdvisoryGroupProjections(context.Context, reducercontract.Intent, []string) error {
	e.calls++
	return nil
}

func TestSupplyChainImpactRegistrationCarriesTheGroupProjectionSeam(t *testing.T) {
	t.Parallel()

	port := &registrationGroupProjectionEnqueuer{}
	definitions := appendSupplyChainCorrelationAdditiveDomains(nil, DefaultHandlers{
		FactLoader: &stubSupplyChainImpactFactLoader{},
		SupplyChainSecurityHandlers: SupplyChainSecurityHandlers{
			SupplyChainImpactWriter:                  &recordingSupplyChainImpactWriter{},
			SupplyChainImpactFencingTokenIssuer:      fixedSupplyChainImpactFencingTokenIssuer{token: 1},
			SupplyChainImpactGroupProjectionEnqueuer: port,
		},
	})
	for _, definition := range definitions {
		if definition.Domain != DomainSupplyChainImpact {
			continue
		}
		handler, ok := definition.Handler.(supplychaincore.SupplyChainImpactHandler)
		if !ok {
			t.Fatalf("handler for %s = %T, want supplychaincore.SupplyChainImpactHandler", definition.Domain, definition.Handler)
		}
		if handler.GroupProjectionEnqueuer == nil {
			t.Fatal("GroupProjectionEnqueuer is nil on the registered handler: touched groups would never enqueue a projection")
		}
		if err := handler.GroupProjectionEnqueuer.EnqueueAdvisoryGroupProjections(context.Background(), reducercontract.Intent{}, []string{"CVE-1"}); err != nil {
			t.Fatalf("EnqueueAdvisoryGroupProjections() error = %v", err)
		}
		if port.calls != 1 {
			t.Fatalf("enqueuer calls = %d, want 1: the registration wired a different port", port.calls)
		}
		return
	}
	t.Fatalf("no %s registration found", DomainSupplyChainImpact)
}
