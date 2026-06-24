// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/governanceaudit"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
	"github.com/eshu-hq/eshu/go/internal/semanticprofile"
	"github.com/eshu-hq/eshu/go/internal/semanticqueue"
)

func TestSemanticWorkerSkipsDeniedProviderEgress(t *testing.T) {
	t.Parallel()

	claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{
		semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation),
	}}
	client := &fakeSemanticProviderClient{enabled: true}
	audit := &fakeGovernanceAuditAppender{}
	var logs bytes.Buffer
	worker := newSemanticWorker(claimer, client, audit, SemanticProviderWorkerConfig{
		ExecutionEnabled: true,
		Policy:           semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionDeny),
	}, &logs)

	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := client.dispatchCalls.Load(); got != 0 {
		t.Fatalf("dispatch calls = %d, want 0 (denied egress must not dispatch)", got)
	}
	if got := len(claimer.skipped); got != 1 {
		t.Fatalf("skipped = %d, want 1", got)
	}
	if got, want := claimer.skipReasons[0], semanticpolicy.ReasonEgressProviderDenied; got != want {
		t.Fatalf("skip reason = %q, want %q", got, want)
	}
	if got := len(audit.events); got != 1 {
		t.Fatalf("audit events = %d, want 1", got)
	}
	event := audit.events[0]
	if got, want := event.Type, governanceaudit.EventTypeSemanticPolicyDecision; got != want {
		t.Fatalf("event.Type = %q, want %q", got, want)
	}
	if got, want := event.Decision, governanceaudit.DecisionDenied; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
	if got, want := event.ReasonCode, semanticpolicy.ReasonEgressProviderDenied; got != want {
		t.Fatalf("event.ReasonCode = %q, want %q", got, want)
	}
	if _, err := governanceaudit.NormalizeEvent(event); err != nil {
		t.Fatalf("NormalizeEvent() error = %v, want nil", err)
	}
	assertNoSecretLeakInAudit(t, event)
	assertNoSecretLeakInLogs(t, logs.String())
}

func TestSemanticWorkerSkipsWithoutSemanticEgressPolicy(t *testing.T) {
	t.Parallel()

	claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{
		semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation),
	}}
	client := &fakeSemanticProviderClient{enabled: true}
	audit := &fakeGovernanceAuditAppender{}
	// Policy enabled but with NO egress rules -> egress policy missing -> fail-closed.
	policy := semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionAllow)
	policy.Egress = semanticpolicy.EgressPolicy{}
	worker := newSemanticWorker(claimer, client, audit, SemanticProviderWorkerConfig{
		ExecutionEnabled: true,
		Policy:           policy,
	}, nil)

	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := client.dispatchCalls.Load(); got != 0 {
		t.Fatalf("dispatch calls = %d, want 0 (missing egress policy must fail closed)", got)
	}
	if got := len(claimer.skipped); got != 1 {
		t.Fatalf("skipped = %d, want 1", got)
	}
	if got, want := claimer.skipReasons[0], semanticpolicy.ReasonEgressPolicyMissing; got != want {
		t.Fatalf("skip reason = %q, want %q", got, want)
	}
	if got := len(audit.events); got != 1 {
		t.Fatalf("audit events = %d, want 1", got)
	}
	if got, want := audit.events[0].Decision, governanceaudit.DecisionUnavailable; got != want {
		t.Fatalf("event.Decision = %q, want %q", got, want)
	}
}

func TestSemanticWorkerAllowedEgressDefaultDisabledClientTerminatesNoNetwork(t *testing.T) {
	t.Parallel()

	claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{
		semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation),
	}}
	audit := &fakeGovernanceAuditAppender{}
	var logs bytes.Buffer
	// No client supplied -> worker uses DisabledSemanticProviderClient.
	worker := newSemanticWorker(claimer, nil, audit, SemanticProviderWorkerConfig{
		ExecutionEnabled: true, // even with execution enabled, the disabled client makes no call
		Policy:           semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionAllow),
	}, &logs)
	worker.Client = nil

	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(claimer.skipped); got != 0 {
		t.Fatalf("skipped = %d, want 0 (egress allowed)", got)
	}
	if got := len(claimer.deadLetter); got != 1 {
		t.Fatalf("dead-letter = %d, want 1 (provider disabled terminal)", got)
	}
	if got, want := claimer.deadLetter[0].Failure.Class, providerDisabledFailureClass; got != want {
		t.Fatalf("failure class = %q, want %q", got, want)
	}
	if got := len(claimer.succeeded); got != 0 {
		t.Fatalf("succeeded = %d, want 0", got)
	}
	if !strings.Contains(logs.String(), providerDisabledFailureClass) {
		t.Fatalf("logs missing provider-disabled outcome: %q", logs.String())
	}
}

func TestSemanticWorkerDisabledClientNeverDispatchesEvenWhenExecutionEnabled(t *testing.T) {
	t.Parallel()

	disabled := DisabledSemanticProviderClient{}
	if disabled.Enabled() {
		t.Fatal("DisabledSemanticProviderClient.Enabled() = true, want false")
	}
	if _, err := disabled.Dispatch(context.Background(), SemanticDispatchRequest{}); !errors.Is(err, ErrSemanticProviderExecutionNotEnabled) {
		t.Fatalf("Dispatch() error = %v, want ErrSemanticProviderExecutionNotEnabled", err)
	}
}

func TestSemanticWorkerAllowedEgressEnabledClientDispatchesAfterGate(t *testing.T) {
	t.Parallel()

	claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{
		semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation),
	}}
	client := &fakeSemanticProviderClient{enabled: true, responseHash: "sha256:abc"}
	audit := &fakeGovernanceAuditAppender{}
	worker := newSemanticWorker(claimer, client, audit, SemanticProviderWorkerConfig{
		ExecutionEnabled: true,
		Policy:           semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionAllow),
	}, nil)

	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := client.dispatchCalls.Load(); got != 1 {
		t.Fatalf("dispatch calls = %d, want 1", got)
	}
	if got := len(claimer.succeeded); got != 1 {
		t.Fatalf("succeeded = %d, want 1", got)
	}
	if got, want := claimer.succeeded[0].ResponseHash, "sha256:abc"; got != want {
		t.Fatalf("response hash = %q, want %q", got, want)
	}
	if got := len(claimer.skipped); got != 0 {
		t.Fatalf("skipped = %d, want 0", got)
	}
}

func TestSemanticWorkerExecutionDisabledFlagBlocksEnabledClient(t *testing.T) {
	t.Parallel()

	claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{
		semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation),
	}}
	client := &fakeSemanticProviderClient{enabled: true}
	worker := newSemanticWorker(claimer, client, &fakeGovernanceAuditAppender{}, SemanticProviderWorkerConfig{
		ExecutionEnabled: false, // default-OFF flag: even an enabled client must not dispatch
		Policy:           semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionAllow),
	}, nil)

	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := client.dispatchCalls.Load(); got != 0 {
		t.Fatalf("dispatch calls = %d, want 0 (execution flag OFF)", got)
	}
	if got := len(claimer.deadLetter); got != 1 {
		t.Fatalf("dead-letter = %d, want 1 (provider disabled terminal)", got)
	}
}

func TestSemanticWorkerDisabledByDefault(t *testing.T) {
	t.Parallel()

	claimer := &fakeSemanticClaimer{pending: []semanticqueue.Record{
		semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation),
	}}
	client := &fakeSemanticProviderClient{enabled: true}
	worker := SemanticProviderWorker{
		Config:  SemanticProviderWorkerConfig{Enabled: false, ScopeIDs: []string{semanticWorkerScope}},
		Claimer: claimer,
		Client:  client,
	}
	if err := worker.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if got := len(claimer.skipped) + len(claimer.deadLetter) + len(claimer.succeeded); got != 0 {
		t.Fatalf("disabled worker performed %d mutations, want 0", got)
	}
	if got := client.dispatchCalls.Load(); got != 0 {
		t.Fatalf("dispatch calls = %d, want 0 when worker disabled", got)
	}
}

func TestSemanticWorkerConcurrentClaimLoopProcessesEachJobOnce(t *testing.T) {
	t.Parallel()

	const jobs = 50
	pending := make([]semanticqueue.Record, 0, jobs)
	for i := 0; i < jobs; i++ {
		record := semanticWorkerRecord("semantic-docs-default", semanticprofile.SourceDocumentation)
		record.JobID = record.JobID + ":" + string(rune('a'+i%26)) + time.Duration(i).String()
		pending = append(pending, record)
	}
	claimer := &fakeSemanticClaimer{pending: pending}
	client := &fakeSemanticProviderClient{enabled: true, responseHash: "sha256:abc"}
	worker := newSemanticWorker(claimer, client, &fakeGovernanceAuditAppender{}, SemanticProviderWorkerConfig{
		ExecutionEnabled: true,
		Policy:           semanticWorkerEgressPolicy(semanticpolicy.EgressDecisionAllow),
	}, nil)
	worker.Config.MaxClaimsPerPass = jobs

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = worker.Run(context.Background())
		}(w)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatalf("Run() error = %v, want nil", err)
		}
	}
	// Each job is claimed exactly once by the lease-fenced claimer, so the total
	// terminal dispositions must equal the job count with no double-processing.
	total := len(claimer.succeeded) + len(claimer.skipped) + len(claimer.deadLetter)
	if total != jobs {
		t.Fatalf("terminal dispositions = %d, want %d (no double-processing)", total, jobs)
	}
	if got := int(client.dispatchCalls.Load()); got != jobs {
		t.Fatalf("dispatch calls = %d, want %d", got, jobs)
	}
}

func assertNoSecretLeakInAudit(t *testing.T, event governanceaudit.Event) {
	t.Helper()
	for _, field := range []string{event.ScopeIDHash, event.CorrelationID, event.ReasonCode, event.ServicePrincipalID} {
		lower := strings.ToLower(field)
		for _, forbidden := range []string{"http://", "https://", "://", "token", "secret", "key=", "password"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("audit field %q leaks forbidden substring %q", field, forbidden)
			}
		}
	}
}

func assertNoSecretLeakInLogs(t *testing.T, logs string) {
	t.Helper()
	lower := strings.ToLower(logs)
	for _, forbidden := range []string{"http://", "https://", "credential", "token", "secret", "password"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("logs leak forbidden substring %q: %s", forbidden, logs)
		}
	}
}
