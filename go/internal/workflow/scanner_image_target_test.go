// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import "testing"

func TestScannerImageTargetRequiresImmutableAuthorizedIdentity(t *testing.T) {
	source := ScannerImageSourceBinding{
		SourceObservationID: 1, SourceCollectorInstanceID: "source-instance",
		SourceScopeID: "scope-1", SourceGenerationID: "generation-1",
		Registry: "registry.example.invalid", Repository: "team/service",
		RuntimeDigest:          "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		RuntimeDigestKind:      ScannerRuntimeDigestConfig,
		RuntimeImageReference:  "registry.example.invalid/team/service:release",
		ManifestVerificationID: "verification-1",
	}
	valid := ScannerImageTarget{
		WorkItemID:             "scanner-worker:item-1",
		AuthorizationNamespace: ScannerAuthorizationLocal,
		AuthorizationDomain:    "tenant-a:registry-a",
		ManifestDigest:         "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Sources:                []ScannerImageSourceBinding{source},
	}
	valid.ScanTargetID = ScannerImageTargetScopeID(valid)
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid target: %v", err)
	}
	hosted := valid
	hosted.AuthorizationNamespace = ScannerAuthorizationHosted
	hosted.TenantID = "tenant-a"
	hosted.WorkspaceID = "workspace-a"
	hosted.SubjectClass = "collector"
	hosted.PolicyRevisionHash = "policy-a"
	hosted.ScanTargetID = ScannerImageTargetScopeID(hosted)
	if hosted.ScanTargetID == valid.ScanTargetID {
		t.Fatal("hosted and local boundary share derived scan scope")
	}
	if err := hosted.Validate(); err != nil {
		t.Fatalf("valid hosted target: %v", err)
	}
	for name, change := range map[string]func(*ScannerImageTarget){
		"missing namespace":   func(target *ScannerImageTarget) { target.AuthorizationNamespace = "" },
		"local with tenant":   func(target *ScannerImageTarget) { target.TenantID = "tenant-a" },
		"tag reference":       func(target *ScannerImageTarget) { target.ManifestDigest = "latest" },
		"unknown digest kind": func(target *ScannerImageTarget) { target.Sources[0].RuntimeDigestKind = "index" },
		"missing source ref":  func(target *ScannerImageTarget) { target.Sources[0].RuntimeImageReference = "" },
		"missing source":      func(target *ScannerImageTarget) { target.Sources = nil },
		"spoofed scope":       func(target *ScannerImageTarget) { target.ScanTargetID += "other" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			invalid.Sources = append([]ScannerImageSourceBinding(nil), valid.Sources...)
			change(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatal("invalid target accepted")
			}
		})
	}
	hosted.WorkspaceID = ""
	if err := hosted.Validate(); err == nil {
		t.Fatal("partial hosted boundary accepted")
	}
}
