// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/cli/demo"
)

// cliContractRecordRel is the #8874 CLI-contract extraction record,
// relative to the repository root. It resolves the three open questions
// the census (cli_extraction_inventory.md, pinned by #9361) carried as
// NOT_CHECKED: the oldest/newest supported core<->CLI version pairs plus
// credential handling, the demo Compose overlay and manifest distribution
// to an installed eshu-cli, and the name/versioning/discovery of the
// future core local-host entrypoint for graph/watch.
//
// The path lives in a package-level constant, and the reader resolves it
// through filepath.Join with the runtime.Caller repo root, because the
// lockstep discovery scanner only classifies that shape as a test read:
// the record must stay in specs/ci-docs-lockstep-paths.txt so a docs-only
// edit still runs this test.
const cliContractRecordRel = "docs/internal/evidence/8874-eshu-cli-contract.md"

// cliContractRecordMustName lists strings the contract record must contain:
// the destination repository and issue anchors, the census PR it builds
// on, the migration stage, the credential surfaces, the demo seam, the
// retained entrypoint, and the explicit NOT_CHECKED tail.
var cliContractRecordMustName = []string{
	"eshu-cli",
	"eshu-deploy",
	"#8874",
	"#9361",
	"planned",
	"NOT_CHECKED",
	"## Compatibility matrix",
	"oldest",
	"newest",
	"ESHU_API_KEY",
	"ESHU_MCP_TOKEN",
	"ESHU_COMPONENT_CORE_VERSION",
	"ESHU_DEMO_COMPOSE_FILE",
	"local-host",
	"buildinfo.AppVersion()",
	"traceExitCode",
}

// cliContractRepoRoot resolves the repository root from this test file's
// own path. cmd/eshu is package main so it cannot import the deploy
// package's helper; this is the same four-level climb that helper uses.
func cliContractRepoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// file is go/cmd/eshu/cli_extraction_contract_test.go.
	root := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(file))))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
		return filepath.Dir(root)
	}
	return root
}

func readCLIContractRecord(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(cliContractRepoRoot(t), cliContractRecordRel))
	if err != nil {
		t.Fatalf("read contract record %s: %v", cliContractRecordRel, err)
	}
	return string(body)
}

func TestCLIContractRecordNamesAnchors(t *testing.T) {
	t.Parallel()
	text := readCLIContractRecord(t)
	for _, want := range cliContractRecordMustName {
		if !strings.Contains(text, want) {
			t.Errorf("contract record %s does not name %q", cliContractRecordRel, want)
		}
	}
}

// TestCLIContractPinsDemoSeam binds the record's demo figures to the tree
// constants, so rotating a figure in either direction fails instead of
// silently describing an overlay the CLI no longer resolves.
func TestCLIContractPinsDemoSeam(t *testing.T) {
	t.Parallel()
	text := readCLIContractRecord(t)
	for _, want := range []string{
		fmt.Sprintf("`%s`", demo.DefaultProject),
		fmt.Sprintf("`%s`", demo.ComposeFileName),
		fmt.Sprintf("`%s`", demo.ManifestPath),
		fmt.Sprintf("`%s`", demo.EnvComposeFile),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("contract record %s does not pin demo figure %s", cliContractRecordRel, want)
		}
	}
}

// TestCLIContractPinsWireParity binds the record's wire figures to the
// tree: the envelope MIME the CLI negotiates and the shared exit-code
// table the destination must preserve byte for byte on day one.
func TestCLIContractPinsWireParity(t *testing.T) {
	t.Parallel()
	text := readCLIContractRecord(t)
	if !strings.Contains(text, fmt.Sprintf("`%s`", eshuEnvelopeMIMEType)) {
		t.Errorf("contract record %s does not pin envelope MIME %q", cliContractRecordRel, eshuEnvelopeMIMEType)
	}
	for code, want := range map[string]int{
		"ambiguous":              3,
		"index_building":         4,
		"stale":                  4,
		"capability_degraded":    5,
		"partial":                5,
		"unsupported_capability": 6,
		"invalid_argument":       2,
		"not_found":              2,
		"scope_not_found":        2,
		"something_else":         1,
	} {
		if got := traceExitCode(code); got != want {
			t.Errorf("traceExitCode(%q) = %d, want %d", code, got, want)
		}
	}
}

// TestCLIContractSendsBearerCredential drives the real APIClient against a
// real server and proves the credential handling the record describes: the
// API key travels as a Bearer [REDACTED] header, and the envelope MIME goes
// out on the Accept header that negotiates the canonical envelope.
func TestCLIContractSendsBearerCredential(t *testing.T) {
	t.Parallel()

	var gotAuth, gotAccept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &APIClient{BaseURL: server.URL, APIKey: "probe-key", HTTPClient: server.Client()}
	if err := client.GetEnvelope("/api/v0/health", nil); err != nil {
		t.Fatalf("client.GetEnvelope() error = %v", err)
	}
	if want := "Bearer probe-key"; gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
	if gotAccept != eshuEnvelopeMIMEType {
		t.Errorf("Accept header = %q, want envelope MIME %q", gotAccept, eshuEnvelopeMIMEType)
	}
}

// TestCLIContractResolvesDemoComposeSeam pins the cross-repo seam
// semantics the record promises an installed eshu-cli: an explicit
// ESHU_DEMO_COMPOSE_FILE override wins, and a checkout without the overlay
// fails with guidance that names the variable.
func TestCLIContractResolvesDemoComposeSeam(t *testing.T) {
	t.Parallel()

	const override = "/opt/eshu/demo/docker-compose.demo.yaml"
	if got, err := demo.ResolveComposeFile(t.TempDir(), func(key string) string {
		if key == demo.EnvComposeFile {
			return override
		}
		return ""
	}); err != nil || got != override {
		t.Fatalf("ResolveComposeFile(override) = %q, %v; want %q, nil", got, err, override)
	}

	_, err := demo.ResolveComposeFile(t.TempDir(), func(string) string { return "" })
	if err == nil {
		t.Fatal("ResolveComposeFile(bare temp dir) error = nil, want missing-overlay guidance")
	}
	if !strings.Contains(err.Error(), demo.EnvComposeFile) {
		t.Errorf("ResolveComposeFile error = %q, want guidance naming %s", err, demo.EnvComposeFile)
	}
}

// TestCLIContractKeepsLocalHostInternal pins the entrypoint boundary the
// record keeps out of the cross-repo contract: local-host stays hidden with
// exactly the watch and mcp-stdio subcommands the retained graph/watch
// verbs re-exec, so extracting graph/watch while local-host stays would
// ship a broken command.
func TestCLIContractKeepsLocalHostInternal(t *testing.T) {
	lockCommandTree(t)

	localHost, _, err := rootCmd.Find([]string{"local-host"})
	if err != nil {
		t.Fatalf("rootCmd.Find(local-host) error = %v", err)
	}
	if localHost == nil || localHost.Name() != "local-host" {
		t.Fatalf("local-host command not found in the cobra tree")
	}
	if !localHost.Hidden {
		t.Error("local-host Hidden = false, want true: the re-exec target is never user-typed")
	}
	var names []string
	for _, sub := range localHost.Commands() {
		names = append(names, sub.Name())
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"watch", "mcp-stdio"} {
		if !strings.Contains(joined, want) {
			t.Errorf("local-host subcommands = %v, want %q among them", names, want)
		}
	}
}

// TestCLIContractPinsVersionReporting pins the version basis the v0 matrix
// converges on: the CLI reports buildinfo.AppVersion(), so a same-tree CLI
// and core build always form the single tested pair.
func TestCLIContractPinsVersionReporting(t *testing.T) {
	t.Parallel()
	if got := buildinfo.AppVersion(); got == "" {
		t.Error("buildinfo.AppVersion() is empty, want a non-empty version basis")
	}
}
