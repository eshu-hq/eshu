// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// TestActivationDerivedScopesRequireExplicitLists covers #9311 ruling option C
// cases (a) and (b): a vulnerability activation whose enabled core derive
// section leaves sources or ecosystems blank fails its reconcile before any
// read, with a value-free ERROR log that names the section and list. The
// collector resolves derived scopes against the union of its sections and
// defaults only an empty union, so a per-section host default would plan
// scopes it refuses.
func TestActivationDerivedScopesRequireExplicitLists(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, block, section, list string
	}{
		{
			name: "(a) blank owned ecosystems beside explicit installed debian",
			block: `{"derive_from_owned_packages":{"enabled":true,"sources":["osv"]},` +
				`"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["debian"]}}`,
			section: "derive_from_owned_packages", list: "ecosystems",
		},
		{
			name:    "(b) one section with blank lists",
			block:   `{"derive_from_owned_packages":{"enabled":true}}`,
			section: "derive_from_owned_packages", list: "sources",
		},
		{
			name:    "(b) installed evidence with blank ecosystems",
			block:   `{"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":[]}}`,
			section: "derive_from_installed_evidence", list: "ecosystems",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
			instance := vulnerabilityActivationWithDerive(now, tc.block)
			store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
			ownedReader := ownedViteReader()
			osReader := &fakeOSPackageAdvisoryTargetReader{}
			var logs bytes.Buffer
			service := testVulnerabilityActivationService(t, now, instance, store, ownedReader, `{"mode":"broad"}`)
			service.OSPackageAdvisoryTargetReader = osReader
			service.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

			err := runCancelledReconcile(t, service)
			want := tc.section + "." + tc.list + ": an activation must list " + tc.list + " explicitly"
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("Run() error = %v, want %q", err, want)
			}
			if got := len(ownedReader.requests) + len(osReader.requests); got != 0 {
				t.Fatalf("derived target reads = %d, want 0 after the refusal", got)
			}
			for _, item := range store.enqueuedItems {
				if strings.HasPrefix(item.ScopeID, "vuln-intel://osv/") {
					t.Fatalf("enqueued derived scope %s, want none after the refusal", item.ScopeID)
				}
			}
			for _, want := range []string{
				`"level":"ERROR"`,
				`"collector_instance_id":"vuln-intel-activation"`,
				`"derive_section":"` + tc.section + `"`,
				`"derive_list":"` + tc.list + `"`,
				`"reason":"derive_lists_not_explicit"`,
			} {
				if !strings.Contains(logs.String(), want) {
					t.Fatalf("logs = %s, want %s", logs.String(), want)
				}
			}
			if strings.Contains(logs.String(), "debian") {
				t.Fatalf("logs = %s, want no configured values in the refusal", logs.String())
			}
		})
	}
}

// TestActivationDerivedScopesPlanWithExplicitListsOnBothSections is ruling case
// (c): both core sections with explicit lists plan owned and installed scopes.
func TestActivationDerivedScopesPlanWithExplicitListsOnBothSections(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 27, 22, 15, 0, 0, time.UTC)
	instance := vulnerabilityActivationWithDerive(now,
		`{"derive_from_owned_packages":{"enabled":true,"sources":["osv"],"ecosystems":["npm"]},`+
			`"derive_from_installed_evidence":{"enabled":true,"sources":["osv"],"ecosystems":["alpine"]}}`)
	store := &fakeStore{instances: []workflow.CollectorInstance{instance}}
	osReader := &fakeOSPackageAdvisoryTargetReader{targets: []workflow.OSPackageAdvisoryTarget{{
		Ecosystem: "alpine", PackageName: "openssl", InstalledVersion: "3.1.4-r5", PackageManager: "apk",
		VendorAdvisorySource: "alpine", RepositoryClass: "vendor", FactID: "os-package-openssl", ScopeID: "image://sha256:4444",
	}}}
	service := testVulnerabilityActivationService(t, now, instance, store, ownedViteReader(), `{"mode":"broad"}`)
	service.OSPackageAdvisoryTargetReader = osReader

	if err := runCancelledReconcile(t, service); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	for _, scopeID := range []string{
		"vuln-intel://osv/npm/vite?version=5.4.21",
		"vuln-intel://osv/alpine/openssl?version=3.1.4-r5",
	} {
		if !hasEnqueuedScope(store, scopeID) {
			t.Fatalf("enqueued items = %+v, want %s", store.enqueuedItems, scopeID)
		}
	}
}
