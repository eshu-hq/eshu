package rust

import (
	"reflect"
	"testing"
)

func TestParseCargoCfgManifest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		want cargoCfgManifest
	}{
		{
			name: "package workspace features and target cfg sections",
			text: `
# workspace root
[package]
name = "api-node-boats" # comment after value
version = "0.1.0"

[workspace]
members = ["crates/core", "tools/xtask",] # bounded one-line arrays

[features]
default = ["serde", "tokio?/rt",]
serde = ["dep:serde"]
cli = []

[target.'cfg(unix)'.dependencies]
libc = "0.2"

[target."cfg(windows)".dependencies]
windows-sys = { version = "0.52" }
`,
			want: cargoCfgManifest{
				PackageName:           "api-node-boats",
				WorkspaceMembers:      []string{"crates/core", "tools/xtask"},
				FeatureNames:          []string{"cli", "default", "serde"},
				DefaultFeatureMembers: []string{"serde", "tokio?/rt"},
				TargetCfgSections: []cargoTargetCfgSection{
					{Expression: "cfg(unix)", DependencyKind: "dependencies"},
					{Expression: "cfg(windows)", DependencyKind: "dependencies"},
				},
			},
		},
		{
			name: "comments whitespace and duplicate target expressions stay deterministic",
			text: `
 [ features ]
default=[]
alloc = [ "dep:alloc" ]
std = ["alloc"] # keep the feature name, not members

[target.'cfg(any(target_os = "linux", target_os = "macos"))'.dependencies]
parking_lot = "0.12"

[target.'cfg(any(target_os = "linux", target_os = "macos"))'.dev-dependencies]
tempfile = "3"

[target.'cfg(target_arch = "wasm32")'.build-dependencies]
cc = "1"
`,
			want: cargoCfgManifest{
				FeatureNames:          []string{"alloc", "default", "std"},
				DefaultFeatureMembers: []string{},
				TargetCfgSections: []cargoTargetCfgSection{
					{Expression: `cfg(any(target_os = "linux", target_os = "macos"))`, DependencyKind: "dependencies"},
					{Expression: `cfg(any(target_os = "linux", target_os = "macos"))`, DependencyKind: "dev-dependencies"},
					{Expression: `cfg(target_arch = "wasm32")`, DependencyKind: "build-dependencies"},
				},
			},
		},
		{
			name: "unsupported dynamic values are ignored",
			text: `
[package]
name.workspace = true

[workspace]
members.workspace = true

[features]
default.workspace = true
dynamic = { workspace = true }

[target.'not-cfg'.dependencies]
ignored = "1"
`,
			want: cargoCfgManifest{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseCargoCfgManifest(tt.text)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseCargoCfgManifest() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
