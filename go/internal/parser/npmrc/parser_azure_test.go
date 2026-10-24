// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseKeepsAzureFeedViews pins the Azure Artifacts feed-view form: every
// feed has @Local, @Prerelease, and @Release views that serve npm, so
// `_packaging/<feed>@<view>` is a real registry path. The '@' is escaped to
// %40 so no emitted field holds an '@', and the row stays a registry row.
func TestParseKeepsAzureFeedViews(t *testing.T) {
	t.Parallel()

	for url, want := range map[string]string{
		"https://pkgs.dev.azure.com/myorg/_packaging/my-feed@Release/npm/registry/":                 "https://pkgs.dev.azure.com/myorg/_packaging/my-feed%40Release/npm/registry/",
		"https://pkgs.dev.azure.com/myorg/_packaging/my-feed@release/npm/registry/":                 "https://pkgs.dev.azure.com/myorg/_packaging/my-feed%40release/npm/registry/",
		"https://pkgs.dev.azure.com/myorg/_packaging/my-feed@Local/npm/registry/":                   "https://pkgs.dev.azure.com/myorg/_packaging/my-feed%40Local/npm/registry/",
		"https://pkgs.dev.azure.com/myorg/_packaging/my-feed@Prerelease/npm/registry/":              "https://pkgs.dev.azure.com/myorg/_packaging/my-feed%40Prerelease/npm/registry/",
		"https://pkgs.dev.azure.com/myorg/myproject/_packaging/my-feed@Release/npm/registry/":       "https://pkgs.dev.azure.com/myorg/myproject/_packaging/my-feed%40Release/npm/registry/",
		"https://pkgs.dev.azure.com/contoso/_packaging/MyFeed@Release/npm/registry/":                "https://pkgs.dev.azure.com/contoso/_packaging/MyFeed%40Release/npm/registry/",
		"https://acme.pkgs.visualstudio.com/_packaging/npm-packages@Release/npm/registry/":          "https://acme.pkgs.visualstudio.com/_packaging/npm-packages%40Release/npm/registry/",
		"https://pkgs.dev.azure.com/myorg/_packaging/my-feed/npm/registry/":                         "https://pkgs.dev.azure.com/myorg/_packaging/my-feed/npm/registry/",
		"https://pkgs.dev.azure.com/fabrikam/_packaging/FabrikamFiber/npm/registry/":                "https://pkgs.dev.azure.com/fabrikam/_packaging/FabrikamFiber/npm/registry/",
		"https://pkgs.dev.azure.com/dataweavers/_packaging/DataweaversPublic@Release/npm/registry/": "https://pkgs.dev.azure.com/dataweavers/_packaging/DataweaversPublic%40Release/npm/registry/",
	} {
		rows := rowsOf(t, parseBody(t, "registry="+url+"\n"))
		if len(rows) != 1 || rows[0]["value"] != want || rows[0]["registry_path_withheld"] != nil {
			t.Errorf("%s: rows = %v, want value %s with the path kept", url, rows, want)
			continue
		}
		for _, field := range []string{"value", "registry_host", "registry_path"} {
			if strings.Contains(str(rows[0][field]), "@") {
				t.Errorf("%s: field %s holds an '@'", url, field)
			}
		}
	}
}

// TestParseRejectsAnAtSignOutsideAnAzureFeedView pins the limits of the Azure
// exception: it needs an Azure host, no port, and exactly one '@' that sits in
// the path and not in the authority, query, fragment, or a comment.
func TestParseRejectsAnAtSignOutsideAnAzureFeedView(t *testing.T) {
	t.Parallel()

	const secret = "SecretPw0123456789"
	for name, value := range map[string]string{
		"non azure host":          "https://registry.example.com/org/_packaging/my-feed@Release/npm/registry/",
		"userinfo on azure host":  "https://user:" + secret + "@pkgs.dev.azure.com/org/_packaging/my-feed@Release/npm/registry/",
		"user only on azure host": "https://" + secret + "@pkgs.dev.azure.com/org/_packaging/my-feed@Release/npm/registry/",
		"port on azure host":      "https://pkgs.dev.azure.com:8080/org/_packaging/my-feed@Release/npm/registry/",
		"empty port on azure":     "https://pkgs.dev.azure.com:/org/_packaging/my-feed@Release/npm/registry/",
		"at in the query":         "https://pkgs.dev.azure.com/org/_packaging/my-feed/npm/registry/?e=" + secret + "@x",
		"at in the comment":       "https://pkgs.dev.azure.com/org/_packaging/my-feed/npm/registry/ ; " + secret + "@x",
		"two at signs":            "https://pkgs.dev.azure.com/org/_packaging/my-feed@Release@" + secret + "/npm/registry/",
		"split userinfo":          "https://" + secret + ":/x@pkgs.dev.azure.com/org/_packaging/my-feed/npm/registry/",
		"suffix lookalike host":   "https://pkgs.dev.azure.com.evil.example/org/_packaging/my-feed@Release/npm/registry/",
		"lookalike visualstudio":  "https://evilpkgs.visualstudio.com.example/_packaging/my-feed@Release/npm/registry/",
	} {
		payload := parseBody(t, "registry="+value+"\n")
		raw, _ := json.Marshal(payload)
		if rows := rowsOf(t, payload); len(rows) != 0 {
			t.Errorf("%s: rows = %v, want none", name, rows)
		}
		if strings.Contains(string(raw), secret) {
			t.Errorf("%s: payload holds the secret: %s", name, raw)
		}
	}
}

// TestParseWithholdsAnAzureViewThatIsNotAName pins that the view is checked
// like any slot, and an '@' in another segment never keeps the path.
func TestParseWithholdsAnAzureViewThatIsNotAName(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/org/_packaging/my-feed@" + noDigitToken + "/npm/registry/",
		"/org/_packaging/my-feed@" + hexToken + "/npm/registry/",
		"/org/_packaging/" + noDigitToken + "@Release/npm/registry/",
		"/org/a@b/_packaging/my-feed/npm/registry/",
		"/org/_packaging/my-feed@/npm/registry/",
		"/org/_packaging/@Release/npm/registry/",
	} {
		rows := rowsOf(t, parseBody(t, "registry=https://pkgs.dev.azure.com"+path+"\n"))
		raw, _ := json.Marshal(rows)
		if len(rows) != 1 || rows[0]["registry_path"] != "" || rows[0]["registry_path_withheld"] != true ||
			rows[0]["value"] != "https://pkgs.dev.azure.com" || strings.Contains(string(raw), "@") ||
			strings.Contains(string(raw), noDigitToken) || strings.Contains(string(raw), hexToken) {
			t.Errorf("path %q: rows = %s, want a host-only row", path, raw)
		}
	}
}

// TestParseKeepsPascalCaseNamesAndTheDualstackCodeArtifactHost covers the
// slot-name and host forms real registries use.
func TestParseKeepsPascalCaseNamesAndTheDualstackCodeArtifactHost(t *testing.T) {
	t.Parallel()

	for _, url := range []string{
		"https://nexus.example.com/repository/MyRepo/",
		"https://nexus.example.com/repository/FabrikamFiber/",
		"https://nexus.example.com/repository/DataweaversPublic/",
		"https://my_domain-111122223333.d.codeartifact.us-west-2.on.aws/npm/my-cli-repo/",
		"https://my-domain-111122223333.d.codeartifact.cn-north-1.amazonaws.com.cn/npm/my-cli-repo/",
	} {
		rows := rowsOf(t, parseBody(t, "registry="+url+"\n"))
		if len(rows) != 1 || rows[0]["value"] != url ||
			rows[0]["registry_path_withheld"] != nil {
			t.Errorf("%s: rows = %v, want the path kept", url, rows)
		}
	}
}
