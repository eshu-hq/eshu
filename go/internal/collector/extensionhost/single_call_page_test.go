// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// singleCallRunner exposes only Runner, so it hides RunPagedCollector and
// sends a production runner down the legacy single-call path.
type singleCallRunner struct {
	Runner
}

// ociStdoutRunner is the production OCI adapter with a canned stdout document,
// so the real decode runs without a container runtime.
func ociStdoutRunner(stdout []byte) OCIRunner {
	return OCIRunner{
		ImageRef: ociTestDigest,
		exec: func(_ context.Context, _ string, _ []string, _ []byte, out, _ io.Writer) error {
			_, err := out.Write(stdout)
			return err
		},
	}
}

func pagedSDKResult(item workflow.WorkItem, page *sdkcollector.ResultPage) sdkcollector.Result {
	result := completeResult(item, testSDKFact(item))
	result.ProtocolVersion = collectorSDKProtocolV1Alpha3
	result.Page = page
	return result
}

func marshalResult(t *testing.T, result sdkcollector.Result) []byte {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal(result) error = %v, want nil", err)
	}
	return raw
}

// TestNextClaimedFailsClosedOnPageFromSingleCallRunner proves a claim that
// cannot reassemble pages never commits one page as the whole result. Without
// the guard, a v1alpha3 result passes the SDK validator and the host treats
// page 0 as the complete generation: silent truncation of every later page.
func TestNextClaimedFailsClosedOnPageFromSingleCallRunner(t *testing.T) {
	t.Parallel()

	continuation := &sdkcollector.ResultPage{Cursor: "resume-token-1", More: true, TotalFacts: 2}
	finalPage := &sdkcollector.ResultPage{TotalFacts: 1}

	tests := []struct {
		name   string
		runner func(*testing.T, workflow.WorkItem) Runner
	}{
		{
			name: "continuation page through the shared decoder",
			runner: func(t *testing.T, item workflow.WorkItem) Runner {
				return singleCallRunner{ociStdoutRunner(marshalResult(t, pagedSDKResult(item, continuation)))}
			},
		},
		{
			name: "final page through the shared decoder",
			runner: func(t *testing.T, item workflow.WorkItem) Runner {
				return singleCallRunner{ociStdoutRunner(marshalResult(t, pagedSDKResult(item, finalPage)))}
			},
		},
		{
			name: "continuation page from a runner that bypasses the decoder",
			runner: func(_ *testing.T, item workflow.WorkItem) Runner {
				return &recordingRunner{result: pagedSDKResult(item, continuation)}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			item := testWorkItem()
			recorder := &recordingResultRecorder{}
			source := mustNewSourceWithManifest(t, testManifestV1Alpha3(), tc.runner(t, item), recorder)

			collected, ok, err := source.NextClaimed(context.Background(), item)
			if err == nil || ok {
				t.Fatalf("NextClaimed() ok=%t error=%v, want false and a failure", ok, err)
			}
			if got := collectFacts(t, collected); len(got) != 0 {
				t.Fatalf("collected facts = %d, want 0", len(got))
			}
			var failure extensionFailure
			if !errors.As(err, &failure) {
				t.Fatalf("NextClaimed() error = %#v, want extensionFailure", err)
			}
			if failure.FailureClass() != FailureClassInvalidResult || !failure.TerminalFailure() {
				t.Fatalf("failure class=%q terminal=%t, want %q terminal",
					failure.FailureClass(), failure.TerminalFailure(), FailureClassInvalidResult)
			}
			message := err.Error()
			if !strings.Contains(message, "page") || !strings.Contains(message, "does not support paging") {
				t.Fatalf("error = %q, want it to name the page and the unsupported runner", message)
			}
			if strings.Contains(message, "resume-token-1") {
				t.Fatalf("error = %q, must not carry the satellite cursor", message)
			}
			if len(recorder.results) != 1 {
				t.Fatalf("result records = %d, want exactly 1", len(recorder.results))
			}
			want := ResultRecord{
				CollectorKind: "repo", Origin: ResultOriginHost, State: sdkcollector.ResultTerminal,
				FailureClass: FailureClassInvalidResult,
			}
			if got := recorder.results[0]; got != want {
				t.Fatalf("result record = %+v, want %+v", got, want)
			}
			if got := len(recorder.statuses); got != 0 {
				t.Fatalf("status records = %d, want 0", got)
			}
		})
	}
}

// TestDecodeSDKResultRejectsPageEnvelope pins the shared single-call decoder
// against the SDK module's own paged fixture, and proves output without a page
// still decodes.
func TestDecodeSDKResultRejectsPageEnvelope(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "sdk", "go", "collector", "testdata", "fixtures", "paged_first.json"))
	if err != nil {
		t.Fatalf("os.ReadFile(paged_first) error = %v, want nil", err)
	}
	if _, err := decodeSDKResult(raw); err == nil || !strings.Contains(err.Error(), "does not support paging") {
		t.Fatalf("decodeSDKResult(paged) error = %v, want page rejection", err)
	}

	for _, input := range []string{
		`{"protocol_version":"collector-sdk/v1alpha1","state":"complete"}`,
		`{"protocol_version":"collector-sdk/v1alpha1","state":"complete","page":null}`,
	} {
		result, err := decodeSDKResult([]byte(input))
		if err != nil || result.State != sdkcollector.ResultComplete {
			t.Fatalf("decodeSDKResult(%s) = %+v, %v, want complete", input, result, err)
		}
	}
}

// TestPagedRunnerStillDecodesPageEnvelope proves the guard does not reach the
// paged path: the production OCI adapter decodes the page into the host
// envelope, leaves the embedded SDK field nil, and commits the final page.
func TestPagedRunnerStillDecodesPageEnvelope(t *testing.T) {
	t.Parallel()

	item := testWorkItem()
	stdout := marshalResult(t, pagedSDKResult(item, &sdkcollector.ResultPage{TotalFacts: 1}))

	call, err := decodePagedSDKResult(stdout)
	if err != nil {
		t.Fatalf("decodePagedSDKResult() error = %v, want nil", err)
	}
	if call.Page == nil || call.Page.TotalFacts != 1 {
		t.Fatalf("decodePagedSDKResult() page = %+v, want the host envelope", call.Page)
	}
	if call.Result.Page != nil {
		t.Fatalf("decodePagedSDKResult() embedded page = %+v, want nil", call.Result.Page)
	}

	source := mustNewPagedSource(t, ociStdoutRunner(stdout), 0)
	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil || !ok {
		t.Fatalf("NextClaimed() ok=%t error=%v, want true nil", ok, err)
	}
	if got := collectFacts(t, collected); len(got) != 1 {
		t.Fatalf("collected facts = %d, want 1", len(got))
	}
}
