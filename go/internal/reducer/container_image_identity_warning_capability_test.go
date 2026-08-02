// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestContainerImageIdentityHandlerRequiresWarningLoaderForDemotion(t *testing.T) {
	t.Parallel()

	loader := &containerImageIdentityWarningBlindLoader{
		scopeFacts: []facts.Envelope{
			gitImageRefFact("git-tag", "registry.example.com/team/api:prod"),
		},
	}
	writer := &recordingContainerImageIdentityWriter{}
	handler := ContainerImageIdentityHandler{
		FactLoader:         loader,
		Writer:             writer,
		FencingTokenIssuer: &stubContainerImageIdentityFencingTokenIssuer{tokens: []int64{1}},
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:     "intent-5854-warning-capability",
		ClaimEpoch:   1,
		Domain:       DomainContainerImageIdentity,
		ScopeID:      "repository:synthetic",
		GenerationID: "generation-5854",
		SourceSystem: "git",
		Cause:        "synthetic missing warning capability",
	})
	if err == nil || !strings.Contains(err.Error(), "warning loader") {
		t.Fatalf("Handle() error = %v, want missing warning loader capability", err)
	}
	if writer.calls != 0 {
		t.Fatalf("writer calls = %d, want 0 when completeness evidence is unavailable", writer.calls)
	}
}

type containerImageIdentityWarningBlindLoader struct {
	scopeFacts []facts.Envelope
}

func (l *containerImageIdentityWarningBlindLoader) ListFacts(
	context.Context,
	string,
	string,
) ([]facts.Envelope, error) {
	return append([]facts.Envelope(nil), l.scopeFacts...), nil
}

func (*containerImageIdentityWarningBlindLoader) ListActiveContainerImageIdentityFacts(
	context.Context,
) ([]facts.Envelope, error) {
	return nil, nil
}
