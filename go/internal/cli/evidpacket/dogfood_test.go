// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidpacket

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/packetdogfood"
)

// errReader fails every Read so ReadBenchmark's stdin error path is reachable
// without a real broken pipe.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadBenchmarkReadsStdinWhenPathIsEmpty(t *testing.T) {
	t.Parallel()

	raw, err := ReadBenchmark(strings.NewReader(`{"schema":"x"}`), "")
	if err != nil {
		t.Fatalf("ReadBenchmark: %v", err)
	}
	if string(raw) != `{"schema":"x"}` {
		t.Errorf("stdin bytes = %q, want the reader's full contents", raw)
	}
}

func TestReadBenchmarkTreatsWhitespacePathAsStdin(t *testing.T) {
	t.Parallel()

	raw, err := ReadBenchmark(strings.NewReader("from-stdin"), "   \t ")
	if err != nil {
		t.Fatalf("ReadBenchmark: %v", err)
	}
	if string(raw) != "from-stdin" {
		t.Errorf("bytes = %q, want the stdin contents; a whitespace-only path must not be opened", raw)
	}
}

func TestReadBenchmarkReadsFileWhenPathIsSet(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "benchmark.json")
	if err := os.WriteFile(path, []byte("from-file"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// stdin carries different bytes, so a path win is unambiguous.
	raw, err := ReadBenchmark(strings.NewReader("from-stdin"), path)
	if err != nil {
		t.Fatalf("ReadBenchmark: %v", err)
	}
	if string(raw) != "from-file" {
		t.Errorf("bytes = %q, want the file contents; a set path must win over stdin", raw)
	}
}

func TestReadBenchmarkWrapsMissingFileWithoutHidingTheCause(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "absent.json")
	_, err := ReadBenchmark(strings.NewReader(""), path)
	if err == nil {
		t.Fatal("expected an error for a missing benchmark file")
	}
	// The operator-facing prefix is a contract. Nothing in the repo greps for
	// it, so this assertion and its stdin twin in
	// TestReadBenchmarkWrapsStdinFailure are what a reword trips.
	wantPrefix := `read dogfood benchmark file "` + path + `": `
	if !strings.HasPrefix(err.Error(), wantPrefix) {
		t.Errorf("error = %q, want prefix %q", err, wantPrefix)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("error %q must still unwrap to fs.ErrNotExist", err)
	}
}

func TestReadBenchmarkWrapsStdinFailure(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("pipe went away")
	_, err := ReadBenchmark(errReader{err: sentinel}, "")
	if err == nil {
		t.Fatal("expected an error when stdin fails")
	}
	if !strings.HasPrefix(err.Error(), "read dogfood benchmark from stdin: ") {
		t.Errorf("error = %q, want the stdin prefix", err)
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error %q must unwrap to the reader's cause", err)
	}
}

func TestReadBenchmarkDoesNotTouchStdinForAFilePath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "benchmark.json")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	// A reader that fails every Read proves the file branch never falls through
	// to stdin: if it did, ReadBenchmark would return the wrapped stdin error
	// and the nil check below would fail.
	if _, err := ReadBenchmark(errReader{err: io.ErrUnexpectedEOF}, path); err != nil {
		t.Fatalf("ReadBenchmark: %v", err)
	}
}

func passingVerdict() packetdogfood.Verdict {
	return packetdogfood.Verdict{
		Schema:    packetdogfood.BenchmarkSchema,
		RunKind:   packetdogfood.RunKindFixture,
		RunID:     "run-7",
		Pass:      true,
		TaskCount: 3,
		Families:  []string{"drift", "service_context", "supply_chain_impact"},
		Criteria: []packetdogfood.Criterion{
			{Name: "family_coverage", Status: packetdogfood.CriterionPass, Detail: "all families"},
		},
	}
}

func TestRenderVerdictPassedLayout(t *testing.T) {
	t.Parallel()

	want := "Evidence-packet dogfood PASSED\n" +
		"  run     : run-7 (fixture)\n" +
		"  tasks   : 3\n" +
		"  families: drift, service_context, supply_chain_impact\n" +
		strings.Repeat("-", 44) + "\n" +
		"  [ok] family_coverage: all families\n"
	if got := RenderVerdict(passingVerdict()); got != want {
		t.Errorf("RenderVerdict()\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderVerdictFailedHeaderAndMarker(t *testing.T) {
	t.Parallel()

	verdict := passingVerdict()
	verdict.Pass = false
	verdict.Criteria = []packetdogfood.Criterion{
		{Name: "answer_time", Status: packetdogfood.CriterionFail, Detail: "packet was slower"},
	}
	got := RenderVerdict(verdict)
	if !strings.HasPrefix(got, "Evidence-packet dogfood FAILED\n") {
		t.Errorf("failed verdict must open with the FAILED header, got:\n%s", got)
	}
	if !strings.Contains(got, "  [!!] answer_time: packet was slower\n") {
		t.Errorf("failed criterion must render the [!!] marker, got:\n%s", got)
	}
}

func TestRenderVerdictSkipMarker(t *testing.T) {
	t.Parallel()

	// packetdogfood.Score never emits CriterionSkip, so this glyph is only
	// reachable from a hand-built verdict; pin it so a future skip criterion
	// does not silently render as a blank.
	verdict := passingVerdict()
	verdict.Criteria = []packetdogfood.Criterion{
		{Name: "token_efficiency", Status: packetdogfood.CriterionSkip, Detail: "not applicable"},
	}
	if got := RenderVerdict(verdict); !strings.Contains(got, "  [--] token_efficiency: not applicable\n") {
		t.Errorf("skip criterion must render the [--] marker, got:\n%s", got)
	}
}

func TestRenderVerdictSubstitutesPlaceholderForAnEmptyRunID(t *testing.T) {
	t.Parallel()

	verdict := passingVerdict()
	verdict.RunID = "  "
	if got := RenderVerdict(verdict); !strings.Contains(got, "  run     : <repo> (fixture)\n") {
		t.Errorf("empty run id must render the <repo> placeholder carried over from cmd/eshu, got:\n%s", got)
	}
}

func TestRenderVerdictEndsInANewline(t *testing.T) {
	t.Parallel()

	// The wrapper writes this with fmt.Fprint, so the trailing newline has to
	// come from here or the shell prompt lands on the last criterion line.
	if got := RenderVerdict(passingVerdict()); !strings.HasSuffix(got, "\n") {
		t.Errorf("RenderVerdict must end in a newline, got:\n%q", got)
	}
}

func TestFailureSummaryJoinsEveryFailedCriterion(t *testing.T) {
	t.Parallel()

	verdict := passingVerdict()
	verdict.Criteria = []packetdogfood.Criterion{
		{Name: "family_coverage", Status: packetdogfood.CriterionFail, Detail: "missing drift"},
		{Name: "answer_correctness", Status: packetdogfood.CriterionPass, Detail: "fine"},
		{Name: "answer_time", Status: packetdogfood.CriterionFail, Detail: "too slow"},
	}
	want := "family_coverage: missing drift; answer_time: too slow"
	if got := FailureSummary(verdict); got != want {
		t.Errorf("FailureSummary() = %q, want %q", got, want)
	}
}

func TestFailureSummaryFallsBackWhenNothingFailed(t *testing.T) {
	t.Parallel()

	if got := FailureSummary(passingVerdict()); got != "unknown failure" {
		t.Errorf("FailureSummary() = %q, want %q", got, "unknown failure")
	}
}
