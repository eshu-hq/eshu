// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"strings"
	"testing"

	sdk "github.com/eshu-hq/eshu/sdk/go/collector"
	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// supportedCoreRange is the manifest compatibleCore this template pins,
// matching the sdk-compatibility row for SDK v0.2.0.
const supportedCoreRange = ">=0.0.5 <0.2.0"

// TestCompatibilityMatrixSupportedPair proves the pinned SDK v0.2.0 protocol
// and manifest range pass conformance together.
func TestCompatibilityMatrixSupportedPair(t *testing.T) {
	t.Parallel()

	manifest := loadManifestForMatrix(t)
	if manifest.Spec.CompatibleCore != supportedCoreRange {
		t.Fatalf("CompatibleCore = %q, want %q", manifest.Spec.CompatibleCore, supportedCoreRange)
	}
	if manifest.Spec.Runtime.SDKProtocol != sdk.ProtocolVersionV1Alpha1 {
		t.Fatalf("SDKProtocol = %q, want %q", manifest.Spec.Runtime.SDKProtocol, sdk.ProtocolVersionV1Alpha1)
	}
	if hostRejectsCoreRange(manifest.Spec.CompatibleCore, "0.1.0") {
		t.Fatalf("pinned range %q must cover supported core 0.1.0", manifest.Spec.CompatibleCore)
	}
	result := mustCollect(t, "complete.json", testClaim(), testObservedAt(), "")
	report := conformance.Run(conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{result}, Mode: conformance.ModeFixture})
	if !report.OK() {
		t.Fatalf("supported pair findings = %#v, want passed", report.Findings)
	}
}

// TestCompatibilityMatrixRejectsUnsupportedVersions proves the harness fails
// closed on an unknown wire protocol, an unversioned fact kind, and a
// manifest whose core range no longer covers the running core.
func TestCompatibilityMatrixRejectsUnsupportedVersions(t *testing.T) {
	t.Parallel()

	manifest := loadManifestForMatrix(t)
	complete := mustCollect(t, "complete.json", testClaim(), testObservedAt(), "")

	unsupportedProtocol := complete
	unsupportedProtocol.ProtocolVersion = "collector-sdk/v9alpha9"
	if report := conformance.Run(conformance.Request{Manifest: manifest, Fixtures: []sdk.Result{unsupportedProtocol}, Mode: conformance.ModeFixture}); report.OK() {
		t.Fatal("unsupported protocol: report OK = true, want failed")
	}

	unversioned := loadManifestForMatrix(t)
	unversioned.Spec.EmittedFacts[0].SchemaVersions = nil
	if report := conformance.Run(conformance.Request{Manifest: unversioned, Fixtures: []sdk.Result{complete}, Mode: conformance.ModeFixture}); report.OK() {
		t.Fatal("unversioned fact kind: report OK = true, want failed")
	} else if !hasFinding(report, conformance.FindingManifestInvalid) {
		t.Fatalf("unversioned findings = %#v, want manifest_invalid", report.Findings)
	}

	futureCore := loadManifestForMatrix(t)
	futureCore.Spec.CompatibleCore = ">=9.0.0 <10.0.0"
	if !hostRejectsCoreRange(futureCore.Spec.CompatibleCore, "0.1.0") {
		t.Fatal("future core range must not cover running core 0.1.0")
	}
}

// hostRejectsCoreRange is the template-side mirror of the host's
// spec.compatibleCore enforcement: a manifest whose declared range does not
// cover the running core never activates. It evaluates the space-separated
// comparators the template actually pins (>= lower bound, < upper bound) with
// numeric segment comparison, so the matrix proves the pinned range admits a
// supported core and a future range does not. Full range parsing stays with
// the core host; this locks the template's pin, not the host's parser.
func hostRejectsCoreRange(manifestRange, runningCore string) bool {
	return !coreRangeCovers(manifestRange, runningCore)
}

func coreRangeCovers(manifestRange, runningCore string) bool {
	running := parseCoreVersion(runningCore)
	if running == nil {
		return false
	}
	for _, comparator := range strings.Fields(manifestRange) {
		op := ""
		rest := comparator
		for _, candidate := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(rest, candidate) {
				op = candidate
				rest = strings.TrimPrefix(rest, candidate)
				break
			}
		}
		bound := parseCoreVersion(rest)
		if bound == nil {
			return false
		}
		cmp := compareCoreVersions(running, bound)
		switch op {
		case ">=":
			if cmp < 0 {
				return false
			}
		case "<":
			if cmp >= 0 {
				return false
			}
		case ">":
			if cmp <= 0 {
				return false
			}
		case "<=":
			if cmp > 0 {
				return false
			}
		case "=":
			if cmp != 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func parseCoreVersion(version string) []int {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	if i := strings.IndexAny(version, "-+"); i >= 0 {
		version = version[:i]
	}
	segments := strings.Split(version, ".")
	if len(segments) < 1 || len(segments) > 3 {
		return nil
	}
	numbers := make([]int, 0, len(segments))
	for _, segment := range segments {
		number := 0
		for _, digit := range []byte(segment) {
			if digit < '0' || digit > '9' {
				return nil
			}
			number = number*10 + int(digit-'0')
		}
		numbers = append(numbers, number)
	}
	return numbers
}

func compareCoreVersions(left, right []int) int {
	width := len(left)
	if len(right) > width {
		width = len(right)
	}
	for i := 0; i < width; i++ {
		l, r := 0, 0
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			if l < r {
				return -1
			}
			return 1
		}
	}
	return 0
}

func loadManifestForMatrix(t *testing.T) conformance.Manifest {
	t.Helper()
	return loadManifest(t)
}
