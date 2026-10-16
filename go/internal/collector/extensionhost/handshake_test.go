// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// testSourceConfig builds the source configuration the handshake tests share,
// mirroring mustNewSourceWithManifest without failing on construction so
// rejection paths can assert the error.
func testSourceConfig(manifest component.Manifest) Config {
	return Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
	}
}

// TestSourceHandshakeAcceptsExplicitAlpha2 proves the extension-host
// handshake admits an explicitly configured collector-sdk/v1alpha2 manifest:
// construction succeeds, the launch request and contract carry the alpha2
// protocol, and an alpha2 extension result validates, including a padded
// protocol variant that must normalize rather than bypass the allowlist.
func TestSourceHandshakeAcceptsExplicitAlpha2(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{
		component.CollectorSDKProtocolV1Alpha2,
		" " + component.CollectorSDKProtocolV1Alpha2 + " ",
	} {
		t.Run(protocol, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			result := completeResult(item, testSDKFact(item))
			result.ProtocolVersion = component.CollectorSDKProtocolV1Alpha2
			runner := &recordingRunner{result: result}
			manifest := testManifest()
			manifest.Spec.Runtime.SDKProtocol = protocol
			source := mustNewSourceWithManifest(t, manifest, runner, &recordingStatusRecorder{})

			collected, ok, err := source.NextClaimed(context.Background(), item)
			if err != nil {
				t.Fatalf("NextClaimed() error = %v, want nil for protocol %q", err, protocol)
			}
			if !ok {
				t.Fatalf("NextClaimed() ok = false, want true for protocol %q", protocol)
			}
			if len(runner.requests) != 1 {
				t.Fatalf("runner calls = %d, want 1 for protocol %q", len(runner.requests), protocol)
			}
			request := runner.requests[0]
			if got := strings.TrimSpace(request.ProtocolVersion); got != component.CollectorSDKProtocolV1Alpha2 {
				t.Fatalf("request protocol = %q, want trimmed %q", request.ProtocolVersion, component.CollectorSDKProtocolV1Alpha2)
			}
			if got := strings.TrimSpace(request.Contract.ProtocolVersion); got != component.CollectorSDKProtocolV1Alpha2 {
				t.Fatalf("contract protocol = %q, want trimmed %q", request.Contract.ProtocolVersion, component.CollectorSDKProtocolV1Alpha2)
			}
			if envelopes := collectFacts(t, collected); len(envelopes) != 1 {
				t.Fatalf("collected facts = %d, want 1 for protocol %q", len(envelopes), protocol)
			}
		})
	}
}

// TestSourceHandshakeRejectsUnsupportedProtocol proves the extension-host
// handshake still fails closed for unknown protocols: construction rejects
// the manifest before any extension launch.
func TestSourceHandshakeRejectsUnsupportedProtocol(t *testing.T) {
	t.Parallel()

	for _, protocol := range []string{"collector-sdk/v9", " collector-sdk/v9 ", ""} {
		t.Run("protocol="+protocol, func(t *testing.T) {
			t.Parallel()

			manifest := testManifest()
			manifest.Spec.Runtime.SDKProtocol = protocol
			_, err := NewSource(testSourceConfig(manifest))
			if err == nil {
				t.Fatalf("NewSource() error = nil, want rejection for protocol %q", protocol)
			}
			if !strings.Contains(err.Error(), "sdkProtocol") {
				t.Fatalf("NewSource() error = %v, want SDK protocol error for %q", err, protocol)
			}
			if protocol != "" && !strings.Contains(err.Error(), protocol) {
				t.Fatalf("NewSource() error = %v, want raw protocol value %q", err, protocol)
			}
		})
	}
}
