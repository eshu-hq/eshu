// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
)

const ociTestDigest = "ghcr.io/eshu-hq/examples/scorecard-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func ociTestRequest() Request {
	item := testWorkItem()
	return Request{
		ProtocolVersion: sdkcollector.ProtocolVersionV1Alpha1,
		Claim:           testSDKClaim(item),
		Contract:        testContract(),
		Config:          map[string]any{"fixture": "scorecard"},
	}
}

func TestOCIRunnerCommandArgsIsDigestPinnedAndIsolated(t *testing.T) {
	t.Parallel()

	runner := OCIRunner{ImageRef: ociTestDigest, Env: []string{"SCORECARD_TOKEN=x"}}
	args, err := runner.commandArgs()
	if err != nil {
		t.Fatalf("commandArgs() error = %v, want nil", err)
	}
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"run --rm --interactive",
		"--network none",
		"--read-only",
		"--user 65532:65532",
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--env SCORECARD_TOKEN=x",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %v", want, args)
		}
	}
	// The digest-pinned image must be the final argument.
	if args[len(args)-1] != ociTestDigest {
		t.Fatalf("last arg = %q, want image %q", args[len(args)-1], ociTestDigest)
	}
}

func TestOCIRunnerCommandArgsRejectsUnpinnedImage(t *testing.T) {
	t.Parallel()

	for _, image := range []string{
		"",
		"ghcr.io/eshu-hq/examples/scorecard-collector:latest",
		"ghcr.io/eshu-hq/examples/scorecard-collector",
		"ghcr.io/eshu-hq/examples/scorecard-collector@sha256:short",
	} {
		runner := OCIRunner{ImageRef: image}
		if _, err := runner.commandArgs(); err == nil {
			t.Fatalf("commandArgs(%q) error = nil, want digest-pin rejection", image)
		}
	}
}

func TestOCIRunnerRunCollectorPipesRequestAndDecodesResult(t *testing.T) {
	t.Parallel()

	var gotName string
	var gotArgs []string
	var gotStdin []byte
	runner := OCIRunner{
		ImageRef: ociTestDigest,
		Runtime:  "podman",
		exec: func(_ context.Context, name string, args []string, stdin []byte, stdout, _ io.Writer) error {
			gotName = name
			gotArgs = args
			gotStdin = stdin
			_, _ = stdout.Write([]byte(`{"protocol_version":"` + sdkcollector.ProtocolVersionV1Alpha1 + `","state":"completed"}`))
			return nil
		},
	}

	result, err := runner.RunCollector(context.Background(), ociTestRequest())
	if err != nil {
		t.Fatalf("RunCollector() error = %v, want nil", err)
	}
	if result.ProtocolVersion != sdkcollector.ProtocolVersionV1Alpha1 {
		t.Fatalf("ProtocolVersion = %q, want %q", result.ProtocolVersion, sdkcollector.ProtocolVersionV1Alpha1)
	}
	if gotName != "podman" {
		t.Fatalf("runtime = %q, want podman", gotName)
	}
	if gotArgs[len(gotArgs)-1] != ociTestDigest {
		t.Fatalf("last arg = %q, want digest", gotArgs[len(gotArgs)-1])
	}
	var decoded Request
	if err := json.Unmarshal(gotStdin, &decoded); err != nil {
		t.Fatalf("request stdin not valid JSON: %v", err)
	}
	if decoded.ProtocolVersion != sdkcollector.ProtocolVersionV1Alpha1 {
		t.Fatalf("stdin request protocol = %q, want %q", decoded.ProtocolVersion, sdkcollector.ProtocolVersionV1Alpha1)
	}
}

func TestOCIRunnerRunCollectorRejectsUnpinnedImageBeforeExec(t *testing.T) {
	t.Parallel()

	called := false
	runner := OCIRunner{
		ImageRef: "ghcr.io/eshu-hq/examples/scorecard-collector:latest",
		exec: func(context.Context, string, []string, []byte, io.Writer, io.Writer) error {
			called = true
			return nil
		},
	}
	if _, err := runner.RunCollector(context.Background(), ociTestRequest()); err == nil {
		t.Fatal("RunCollector() error = nil, want digest-pin rejection")
	}
	if called {
		t.Fatal("container runtime was invoked for an unpinned image")
	}
}

func TestOCIRunnerRunCollectorWrapsExecError(t *testing.T) {
	t.Parallel()

	runner := OCIRunner{
		ImageRef: ociTestDigest,
		exec: func(_ context.Context, _ string, _ []string, _ []byte, _, stderr io.Writer) error {
			_, _ = stderr.Write([]byte("boom"))
			return errors.New("exit status 1")
		},
	}
	_, err := runner.RunCollector(context.Background(), ociTestRequest())
	if err == nil || !strings.Contains(err.Error(), "extension oci adapter failed") {
		t.Fatalf("RunCollector() error = %v, want wrapped adapter failure", err)
	}
}

func TestOCIRunnerRunCollectorEnforcesStdoutLimit(t *testing.T) {
	t.Parallel()

	runner := OCIRunner{
		ImageRef:         ociTestDigest,
		StdoutLimitBytes: 8,
		exec: func(_ context.Context, _ string, _ []string, _ []byte, stdout, _ io.Writer) error {
			_, _ = stdout.Write([]byte(`{"protocol_version":"collector-sdk/v1alpha1","state":"completed"}`))
			return nil
		},
	}
	if _, err := runner.RunCollector(context.Background(), ociTestRequest()); err == nil ||
		!strings.Contains(err.Error(), "stdout limit exceeded") {
		t.Fatalf("RunCollector() error = %v, want stdout limit error", err)
	}
}

func TestOCIRunnerRunCollectorHonorsCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	runner := OCIRunner{
		ImageRef: ociTestDigest,
		exec: func(context.Context, string, []string, []byte, io.Writer, io.Writer) error {
			return errors.New("killed")
		},
	}
	if _, err := runner.RunCollector(ctx, ociTestRequest()); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunCollector() error = %v, want context.Canceled", err)
	}
}

// OCIRunner must satisfy the Runner interface so the component-extension
// collector can dispatch through it identically to the process adapter.
var _ Runner = OCIRunner{}
