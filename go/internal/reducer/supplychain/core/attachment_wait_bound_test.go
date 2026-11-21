// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/securityalert"
)

// TestSupplyChainImpactAttachmentWaitThenOwnDigestBoundYieldsLateFindings
// joins #11105 to the #11087 own-digest bound (arbiter ruling item 5). It is
// fixture c of criteria_expansion_image_arrival_test.go through Handle: the
// image scope's pass is claimed while its own attachment is still in flight,
// so it defers and writes nothing; once the attachment commits in the image
// scope, the same pass, bounded, publishes every finding the attachment's
// document anchors on the image scope's own digest. Without the wait the
// first pass would have run on the attachment-less universe and published
// none of them, with nothing to re-run it.
func TestSupplyChainImpactAttachmentWaitThenOwnDigestBoundYieldsLateFindings(t *testing.T) {
	t.Parallel()

	base := buildSharedDocumentUniverse("0.10", true)
	attInImage := base.moveAttachment(sdScopeM, sdScopeIM)
	earlyImage := attInImage.withoutAttachment()
	scopes := []string{sdScopeB, sdScopeKB, sdScopeM, sdScopeIM}

	// The findings the late attachment adds to the image scope's own run.
	before := sdRun(t, earlyImage, sdScopeIM, true, scopes...)
	after := sdRun(t, attInImage, sdScopeIM, true, scopes...)
	var late []string
	for key := range after {
		if _, ok := before[key]; !ok {
			late = append(late, key)
		}
	}
	if len(late) == 0 {
		t.Fatalf("control: the late attachment adds no own-digest finding (before=%s after=%s)", sdKeys(before), sdKeys(after))
	}

	t.Logf("late attachment adds %d own-digest findings to the image scope run", len(late))

	port := &stubAttachmentReadiness{state: securityalert.ImpactProducerState{
		Retrying: 1, Total: 1, NewestInFlightWorkableAt: attachmentWaitNow.Add(time.Minute),
	}}
	handler, writer := boundedImageScopeHandler(earlyImage.present(scopes...), port)
	intent := attachmentWaitIntent()
	intent.ScopeID = sdScopeIM
	if _, err := handler.Handle(context.Background(), intent); err == nil ||
		failureClassOfCore(err) != SBOMAttachmentNotReadyFailureClass {
		t.Fatalf("first pass error = %v, want the %s deferral", err, SBOMAttachmentNotReadyFailureClass)
	}
	if writer.calls != 0 {
		t.Fatalf("deferred pass wrote %d times, want 0", writer.calls)
	}

	// The attachment committed and acked: the reopened pass reads the
	// universe that now holds it.
	port.state = securityalert.ImpactProducerState{Succeeded: 1, Total: 1}
	handler, writer = boundedImageScopeHandler(attInImage.present(scopes...), port)
	if _, err := handler.Handle(context.Background(), intent); err != nil {
		t.Fatalf("reopened pass error = %v", err)
	}
	if writer.calls != 1 {
		t.Fatalf("reopened pass wrote %d times, want 1", writer.calls)
	}
	written := sdProject(t, writer.write.Findings)
	for _, key := range late {
		got, ok := written[key]
		if !ok {
			t.Fatalf("reopened bounded pass is missing late finding %q; wrote %s", key, sdKeys(written))
		}
		if got.Digest != after[key].Digest || got.Repository != after[key].Repository {
			t.Fatalf("late finding %q anchored on %s/%s, want %s/%s", key, got.Digest, got.Repository, after[key].Digest, after[key].Repository)
		}
	}
}

// boundedImageScopeHandler builds the image scope's handler over a universe
// with the #11087 criteria bound on and the #11105 port wired.
func boundedImageScopeHandler(
	all []facts.Envelope,
	port SBOMAttachmentReadiness,
) (SupplyChainImpactHandler, *recordingSupplyChainImpactWriter) {
	var own []facts.Envelope
	for _, envelope := range all {
		if envelope.ScopeID == sdScopeIM {
			own = append(own, envelope)
		}
	}
	writer := &recordingSupplyChainImpactWriter{}
	return SupplyChainImpactHandler{
		FactLoader:             &stubSupplyChainImpactFactLoader{scopeFacts: own, activeForFilter: sdLoader(all)},
		Writer:                 writer,
		FencingTokenIssuer:     &countingIssuer{},
		CriteriaExpansionBound: &SupplyChainImpactCriteriaExpansionBound{},
		AttachmentReadiness:    port,
		Now:                    func() time.Time { return attachmentWaitNow },
	}, writer
}

// failureClassOfCore reads the queue failure class an error carries.
func failureClassOfCore(err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	return ""
}
