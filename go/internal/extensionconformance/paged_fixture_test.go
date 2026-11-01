// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionconformance

import (
	"context"
	"os"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
)

// TestRunFixtureModeJudgesPagedFixtures pins the conformance verdicts for page
// envelopes, which depend on readFixture keeping the page the fixture carries.
func TestRunFixtureModeJudgesPagedFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		protocol string
		page     *sdkcollector.ResultPage
		wantCode FindingCode
	}{
		{
			name:     "v1alpha3 manifest with a valid page passes",
			protocol: sdkcollector.ProtocolVersionV1Alpha3,
			page:     &sdkcollector.ResultPage{Cursor: "page-cursor-1", More: true, TotalFacts: 2},
		},
		{
			name:     "v1alpha1 manifest with a page fails",
			protocol: sdkcollector.ProtocolVersionV1Alpha1,
			page:     &sdkcollector.ResultPage{Cursor: "page-cursor-1", More: true, TotalFacts: 2},
			wantCode: FindingFixtureContractFailed,
		},
		{
			name:     "v1alpha3 continuation without a cursor fails",
			protocol: sdkcollector.ProtocolVersionV1Alpha3,
			page:     &sdkcollector.ResultPage{More: true, TotalFacts: 2},
			wantCode: FindingFixtureContractFailed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := validConformanceResult()
			result.ProtocolVersion = tc.protocol
			result.Page = tc.page

			report, err := Run(context.Background(), Request{
				ManifestPath:  writeConformanceManifestForProtocol(t, tc.protocol),
				FixturePaths:  []string{writeConformanceFixture(t, result)},
				Mode:          ModeFixture,
				ComponentHome: t.TempDir(),
			})
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("Run() error = %v, want nil; findings=%#v", err, report.Findings)
				}
				if got, want := report.Status, StatusPassed; got != want {
					t.Fatalf("Status = %q, want %q; findings=%#v", got, want, report.Findings)
				}
				return
			}
			if err == nil {
				t.Fatalf("Run() error = nil, want finding %q; status=%q", tc.wantCode, report.Status)
			}
			assertFinding(t, report, tc.wantCode, true, true)
		})
	}
}

// writeConformanceManifestForProtocol writes the standard test manifest with
// spec.runtime.sdkProtocol set to protocol.
func writeConformanceManifestForProtocol(t *testing.T, protocol string) string {
	t.Helper()

	path := writeConformanceManifest(t, "source_evidence_only:no_graph_truth")
	raw, err := os.ReadFile(path) // #nosec G304 -- reads the temp manifest this helper just wrote
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	const legacy = "sdkProtocol: " + sdkcollector.ProtocolVersionV1Alpha1
	body := strings.Replace(string(raw), legacy, "sdkProtocol: "+protocol, 1)
	if protocol != sdkcollector.ProtocolVersionV1Alpha1 && body == string(raw) {
		t.Fatalf("manifest did not name %q", legacy)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	return path
}
