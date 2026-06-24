// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPackageRegistryListPackagesClassifiesBlankPackageIdentityRows(t *testing.T) {
	t.Parallel()

	reader := &recordingPackageRegistryGraphReader{
		runRows: []map[string]any{
			{
				"package_id":         "",
				"ecosystem":          "npm",
				"registry":           "registry.npmjs.org",
				"namespace":          "@bad",
				"normalized_name":    "@bad/missing-id",
				"purl":               "pkg:npm/%40bad/missing-id",
				"bom_ref":            "pkg:npm/%40bad/missing-id",
				"package_manager":    "npm",
				"source_path":        "package.json",
				"source_specific_id": "npm:@bad/missing-id",
				"version_count":      int64(0),
			},
			{
				"package_id":      "npm://registry.npmjs.org/left-pad",
				"ecosystem":       "npm",
				"registry":        "registry.npmjs.org",
				"normalized_name": "left-pad",
				"purl":            "pkg:npm/left-pad",
				"bom_ref":         "pkg:npm/left-pad",
				"package_manager": "npm",
				"version_count":   int64(0),
			},
		},
	}
	handler := &PackageRegistryHandler{Neo4j: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/package-registry/packages?ecosystem=npm&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp struct {
		Packages []PackageRegistryPackageResult `json:"packages"`
		Issues   []struct {
			Reason          string   `json:"reason"`
			MissingEvidence []string `json:"missing_evidence"`
			Ecosystem       string   `json:"ecosystem"`
			Registry        string   `json:"registry"`
			Namespace       string   `json:"namespace"`
			NormalizedName  string   `json:"normalized_name"`
			PURL            string   `json:"purl"`
			BOMRef          string   `json:"bom_ref"`
			PackageManager  string   `json:"package_manager"`
			SourcePath      string   `json:"source_path"`
			SourceSpecific  string   `json:"source_specific_id"`
			VersionCount    int      `json:"version_count"`
		} `json:"identity_issues"`
		Count int `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Packages), 1; got != want {
		t.Fatalf("len(packages) = %d, want %d: %#v", got, want, resp.Packages)
	}
	if got, want := resp.Packages[0].PackageID, "npm://registry.npmjs.org/left-pad"; got != want {
		t.Fatalf("package_id = %q, want %q", got, want)
	}
	if got, want := resp.Packages[0].VersionCount, 0; got != want {
		t.Fatalf("valid zero-version package version_count = %d, want %d", got, want)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := len(resp.Issues), 1; got != want {
		t.Fatalf("len(identity_issues) = %d, want %d; body = %s", got, want, w.Body.String())
	}
	issue := resp.Issues[0]
	if got, want := issue.Reason, "package_id_missing"; got != want {
		t.Fatalf("identity issue reason = %q, want %q", got, want)
	}
	if got, want := issue.MissingEvidence, []string{"package_id"}; !equalStringSlices(got, want) {
		t.Fatalf("missing_evidence = %#v, want %#v", got, want)
	}
	if got, want := issue.Ecosystem, "npm"; got != want {
		t.Fatalf("identity issue ecosystem = %q, want %q", got, want)
	}
	if got, want := issue.Registry, "registry.npmjs.org"; got != want {
		t.Fatalf("identity issue registry = %q, want %q", got, want)
	}
	if got, want := issue.Namespace, "@bad"; got != want {
		t.Fatalf("identity issue namespace = %q, want %q", got, want)
	}
	if got, want := issue.NormalizedName, "@bad/missing-id"; got != want {
		t.Fatalf("identity issue normalized_name = %q, want %q", got, want)
	}
	if got, want := issue.PURL, "pkg:npm/%40bad/missing-id"; got != want {
		t.Fatalf("identity issue purl = %q, want %q", got, want)
	}
	if got, want := issue.BOMRef, "pkg:npm/%40bad/missing-id"; got != want {
		t.Fatalf("identity issue bom_ref = %q, want %q", got, want)
	}
	if got, want := issue.PackageManager, "npm"; got != want {
		t.Fatalf("identity issue package_manager = %q, want %q", got, want)
	}
	if got, want := issue.SourcePath, "package.json"; got != want {
		t.Fatalf("identity issue source_path = %q, want %q", got, want)
	}
	if got, want := issue.SourceSpecific, "npm:@bad/missing-id"; got != want {
		t.Fatalf("identity issue source_specific_id = %q, want %q", got, want)
	}
	if got, want := issue.VersionCount, 0; got != want {
		t.Fatalf("identity issue version_count = %d, want %d", got, want)
	}
}

func TestPackageRegistryIdentityIssueSerializesMissingEvidenceField(t *testing.T) {
	t.Parallel()

	payload, err := json.Marshal(PackageRegistryIdentityIssue{
		Reason:          packageRegistryPackageIDMissingReason,
		MissingEvidence: []string{},
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if _, ok := decoded["missing_evidence"]; !ok {
		t.Fatalf("missing_evidence omitted from %s", payload)
	}
}

func TestPackageRegistryListPackagesPreservesZeroVersionNPMIdentities(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		queryName  string
		packageID  string
		namespace  string
		normalized string
	}{
		{
			name:       "scoped",
			queryName:  "@eshu/core-api",
			packageID:  "npm://registry.npmjs.org/@eshu/core-api",
			namespace:  "@eshu",
			normalized: "@eshu/core-api",
		},
		{
			name:       "unscoped",
			queryName:  "left-pad",
			packageID:  "npm://registry.npmjs.org/left-pad",
			normalized: "left-pad",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reader := &recordingPackageRegistryGraphReader{
				runRows: []map[string]any{
					{
						"package_id":      tc.packageID,
						"ecosystem":       "npm",
						"registry":        "registry.npmjs.org",
						"namespace":       tc.namespace,
						"normalized_name": tc.normalized,
						"purl":            "pkg:npm/" + tc.normalized,
						"bom_ref":         "pkg:npm/" + tc.normalized,
						"package_manager": "npm",
						"version_count":   int64(0),
					},
				},
			}
			handler := &PackageRegistryHandler{Neo4j: reader}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(
				http.MethodGet,
				"/api/v0/package-registry/packages?ecosystem=npm&name="+tc.queryName+"&limit=10",
				nil,
			)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got, want := reader.lastParams["name"], tc.queryName; got != want {
				t.Fatalf("params[name] = %#v, want %#v", got, want)
			}
			var resp struct {
				Packages []PackageRegistryPackageResult `json:"packages"`
				Issues   []any                          `json:"identity_issues"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("json.Unmarshal: %v", err)
			}
			if got, want := len(resp.Packages), 1; got != want {
				t.Fatalf("len(packages) = %d, want %d; body = %s", got, want, w.Body.String())
			}
			if got, want := resp.Packages[0].PackageID, tc.packageID; got != want {
				t.Fatalf("package_id = %q, want %q", got, want)
			}
			if got, want := resp.Packages[0].VersionCount, 0; got != want {
				t.Fatalf("version_count = %d, want %d", got, want)
			}
			if len(resp.Issues) != 0 {
				t.Fatalf("identity_issues = %#v, want empty", resp.Issues)
			}
		})
	}
}
