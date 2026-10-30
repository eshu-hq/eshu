// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestDotNetManifestContentOnlyDispatch drives the manifestDotNetGlobal and
// manifestDotNetProject Extract arms through the content-only path,
// asserting section mapping, order indexes, and spec kinds.
func TestDotNetManifestContentOnlyDispatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, body, kind, raw, spec string
		line, order                       int
	}{
		{"global exact", "global.json", `{"sdk":{"version":"8.0.300"}}`, "dotnet_global_json_sdk", "8.0.300", "exact", 1, 0},
		{"global range", "global.json", `{"sdk":{"version":">=8.0 <9.0"}}`, "dotnet_global_json_sdk", ">=8.0 <9.0", "range", 1, 0},
		{"global alias", "global.json", `{"sdk":{"version":"preview"}}`, "dotnet_global_json_sdk", "preview", "alias", 1, 0},
		{"global unresolved", "global.json", `{"sdk":{"version":"${SDK}"}}`, "dotnet_global_json_sdk", "${SDK}", "unresolved", 1, 0},
		{"csproj single", "App.csproj", `<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup></Project>`, "dotnet_csproj_target_framework", "net8.0", "alias", 1, 0},
		{"csproj exact", "App.csproj", `<Project><PropertyGroup><TargetFramework>8.0.300</TargetFramework></PropertyGroup></Project>`, "dotnet_csproj_target_framework", "8.0.300", "exact", 1, 0},
		{"nested global", "services/api/global.json", "{\n  \"sdk\": {\n    \"version\": \"8.0.300\"\n  }\n}", "dotnet_global_json_sdk", "8.0.300", "exact", 3, 0},
		{"nested csproj", "services/api/App.csproj", "<Project Sdk=\"Microsoft.NET.Sdk\">\n  <PropertyGroup>\n    <TargetFrameworks>net8.0;net9.0</TargetFrameworks>\n  </PropertyGroup>\n</Project>", "dotnet_csproj_target_frameworks", "net8.0", "alias", 3, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, tc.path, tc.body)
			if len(got) == 0 {
				t.Fatalf("Emit(%q) emitted nothing", tc.path)
			}
			first := got[0]
			if first.SourceKind != tc.kind || first.RawSpec != tc.raw || first.SpecKind != tc.spec || first.Runtime != "dotnet" || first.LineNumber != tc.line || first.OrderIndex != tc.order {
				t.Fatalf("fact = %+v, want kind=%s raw=%s spec=%s line=%d order=%d", first, tc.kind, tc.raw, tc.spec, tc.line, tc.order)
			}
		})
	}
}

// TestDotNetManifestTargetFrameworksOrderIndexes proves one TargetFrameworks
// row with two entries emits two facts with order 0 and 1.
func TestDotNetManifestTargetFrameworksOrderIndexes(t *testing.T) {
	t.Parallel()
	got := emitDecoded(t, "App.csproj", `<Project><PropertyGroup><TargetFrameworks>net8.0;net9.0</TargetFrameworks></PropertyGroup></Project>`)
	if len(got) != 2 || got[0].SourceKind != "dotnet_csproj_target_frameworks" || got[1].SourceKind != "dotnet_csproj_target_frameworks" {
		t.Fatalf("declarations = %+v", got)
	}
	if got[0].RawSpec != "net8.0" || got[1].RawSpec != "net9.0" || got[0].OrderIndex != 0 || got[1].OrderIndex != 1 {
		t.Fatalf("order indexes = %+v", got)
	}
	if got[0].SpecKind != "alias" || got[1].SpecKind != "alias" {
		t.Fatalf("spec kinds = %+v", got)
	}
}

// TestDotNetManifestSkips proves malformed and credential bodies skip without
// emitting, mirroring the sibling composer/JVM dispatch tests.
func TestDotNetManifestSkips(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, body, reason string
		rejected                 int
	}{
		{"global malformed", "global.json", `{"sdk":`, SkipReasonManifestUnparseable, 0},
		{"global credential", "global.json", `{"sdk":{"version":"https://user:password@example.invalid"}}`, SkipReasonValueRejected, 1},
		{"csproj malformed", "App.csproj", `<Project><PropertyGroup><TargetFramework>net8.0`, SkipReasonManifestUnparseable, 0},
		{"csproj credential", "App.csproj", `<Project><PropertyGroup><TargetFramework>https://user:password@example.invalid</TargetFramework></PropertyGroup></Project>`, SkipReasonValueRejected, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext(tc.path), "repo-1", tc.path, tc.body)
			if len(envelopes) != 0 || skip.Reason != tc.reason || skip.Rejected != tc.rejected {
				t.Fatalf("emitted %d, skip %+v", len(envelopes), skip)
			}
		})
	}
}

// TestDotNetManifestByteCap proves an over-cap manifest skips whole with the
// manifest byte cap, mirroring the sibling composer byte-cap test.
func TestDotNetManifestByteCap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, path, body string }{
		{"global", "global.json", `{"sdk":{"version":"8.0.300"},"padding":"` + strings.Repeat("x", MaxManifestBytesPerFile) + `"}`},
		{"csproj", "App.csproj", "<Project><PropertyGroup><TargetFramework>net8.0</TargetFramework></PropertyGroup><Padding>" + strings.Repeat("x", MaxManifestBytesPerFile) + "</Padding></Project>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext(tc.path), "repo-1", tc.path, tc.body)
			if len(envelopes) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteSize != len(tc.body) || skip.ByteCap != MaxManifestBytesPerFile {
				t.Fatalf("over-cap %s emitted %d declarations, skip %+v", tc.name, len(envelopes), skip)
			}
		})
	}
}

// TestDotNetOverTokenCapPropagatesMeasuredCount pins the over_tokens skip to
// the measured count under the shared RuntimeVersionTokenCap.
func TestDotNetOverTokenCapPropagatesMeasuredCount(t *testing.T) {
	t.Parallel()
	evidence := shared.NewDotNetRuntimeEvidence([]byte(`{"sdk":{"version":"8.0.300"}}`))
	for i := 0; i < shared.RuntimeVersionTokenCap+1; i++ {
		evidence.Add("dotnet_global_json_sdk", "8.0.300", 1)
	}
	if evidence.State != "over_tokens" {
		t.Fatalf("state = %q, want over_tokens", evidence.State)
	}
	if evidence.TokenCount != shared.RuntimeVersionTokenCap+1 {
		t.Fatalf("TokenCount = %d, want %d", evidence.TokenCount, shared.RuntimeVersionTokenCap+1)
	}
	extraction := ExtractDotNetEvidence(evidence)
	if extraction.Skip().Reason != SkipReasonTokenCap || extraction.Skip().TokenCount != shared.RuntimeVersionTokenCap+1 {
		t.Fatalf("skip = %+v, want token_cap with count %d", extraction.Skip(), shared.RuntimeVersionTokenCap+1)
	}
	if extraction.Len() != 0 {
		t.Fatalf("over-cap extraction holds %d declarations, want none", extraction.Len())
	}
}

// TestDotNetKeepsUpToSharedCap proves values past the old 128 cap are kept
// up to the shared 1024 cap.
func TestDotNetKeepsUpToSharedCap(t *testing.T) {
	t.Parallel()
	evidence := shared.NewDotNetRuntimeEvidence([]byte(`<Project></Project>`))
	for i := 0; i < 200; i++ {
		evidence.Add("dotnet_csproj_target_framework", "8.0.300", 1)
	}
	if evidence.State != "parsed_values" {
		t.Fatalf("state = %q, want parsed_values", evidence.State)
	}
	if len(evidence.Values) != 200 || evidence.TokenCount != 200 {
		t.Fatalf("values = %d tokens = %d, want 200 and 200", len(evidence.Values), evidence.TokenCount)
	}
}
