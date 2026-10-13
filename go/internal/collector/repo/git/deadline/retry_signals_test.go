// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadline

import (
	"errors"
	"strings"
	"testing"
)

// TestRunRetryingSignalsOfARecoveredCommand proves every attempt is counted by
// its class and outcome and logged with the fields an operator needs, and the
// recovery is one INFO line.
func TestRunRetryingSignalsOfARecoveredCommand(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{dnsStderr, "ok"}}
	if err := RunRetrying(h.ctx, fastLimits, h.command(run, Fetch)); err != nil {
		t.Fatalf("RunRetrying() error = %v", err)
	}
	wantSeries(t, h.series(t, attemptsMetric), attemptsMetric, map[string]int64{
		"failure_class=dns_resolve_failed,host=github.com,operation=fetch,outcome=retried": 1,
		"failure_class=none,host=github.com,operation=fetch,outcome=success":               1,
	})
	wantSeries(t, h.series(t, retriesMetric), retriesMetric, map[string]int64{
		"failure_class=dns_resolve_failed,host=github.com,operation=fetch": 1,
	})
	wantSeries(t, h.series(t, durationMetric), durationMetric, map[string]int64{
		"operation=fetch,outcome=retried": 1,
		"operation=fetch,outcome=success": 1,
	})
	if got := h.series(t, exhaustedMetric); len(got) != 0 {
		t.Fatalf("%s = %v, want none", exhaustedMetric, got)
	}

	warns := h.recordsWithMessage(t, "git command attempt failed")
	if len(warns) != 1 {
		t.Fatalf("attempt WARN lines = %d, want 1:\n%s", len(warns), h.logs)
	}
	w := warns[0]
	for key, want := range map[string]any{
		"level": "WARN", "operation": "fetch", "host": "github.com", "repository_id": "github/o/r",
		"attempt": float64(1), "attempts": float64(2), "failure_class": "dns_resolve_failed",
		"deadline_seconds": float64(60), "will_retry": true, "backoff_seconds": float64(3),
		"exit_status": "exit status 128",
	} {
		if w[key] != want {
			t.Errorf("WARN %s = %v, want %v (record %v)", key, w[key], want, w)
		}
	}
	if line, _ := w["error"].(string); !strings.Contains(line, "Could not resolve host") || strings.Contains(line, "\n") {
		t.Errorf("WARN error = %q, want the first error line", line)
	}
	if _, ok := w["elapsed_seconds"]; !ok {
		t.Errorf("WARN has no elapsed_seconds: %v", w)
	}
	recovered := h.recordsWithMessage(t, "git command recovered on attempt 2")
	if len(recovered) != 1 || recovered[0]["level"] != "INFO" {
		t.Fatalf("recovery lines = %v, want one INFO 'recovered on attempt 2'", recovered)
	}
}

// TestRunRetryingSignalsOfAnExhaustedCommand proves the failed attempts are
// counted as retried then failed with the same class, the retry counter moves
// once, and the final WARN says it will not retry.
func TestRunRetryingSignalsOfAnExhaustedCommand(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{dnsStderr}}
	if err := RunRetrying(h.ctx, fastLimits, h.command(run, Resolve)); err == nil {
		t.Fatal("RunRetrying() = nil, want a failure")
	}
	wantSeries(t, h.series(t, attemptsMetric), attemptsMetric, map[string]int64{
		"failure_class=dns_resolve_failed,host=github.com,operation=resolve,outcome=retried": 1,
		"failure_class=dns_resolve_failed,host=github.com,operation=resolve,outcome=failed":  1,
	})
	wantSeries(t, h.series(t, retriesMetric), retriesMetric, map[string]int64{
		"failure_class=dns_resolve_failed,host=github.com,operation=resolve": 1,
	})
	warns := h.recordsWithMessage(t, "git command attempt failed")
	if len(warns) != 2 || warns[0]["will_retry"] != true || warns[1]["will_retry"] != false {
		t.Fatalf("WARN lines = %v, want two with will_retry true then false", warns)
	}
}

// TestRunRetryingSignalsOfANonRetryableFailure proves a failure the table
// names but does not retry is still one counted, logged attempt.
func TestRunRetryingSignalsOfANonRetryableFailure(t *testing.T) {
	h := newRetryHarness(t, fastLimits)
	run := &scripted{outcome: []string{authStderr}}
	if err := RunRetrying(h.ctx, fastLimits, h.command(run, Fetch)); err == nil {
		t.Fatal("RunRetrying() = nil, want a failure")
	}
	wantSeries(t, h.series(t, attemptsMetric), attemptsMetric, map[string]int64{
		"failure_class=auth_failed,host=github.com,operation=fetch,outcome=failed": 1,
	})
	warns := h.recordsWithMessage(t, "git command attempt failed")
	if len(warns) != 1 || warns[0]["failure_class"] != "auth_failed" || warns[0]["will_retry"] != false {
		t.Fatalf("WARN lines = %v, want one auth_failed line with will_retry=false", warns)
	}
}

// TestFirstErrorLine proves the logged "first line of the underlying error" is
// the error, not the --progress noise that comes before it in a failed fetch or
// clone (#8091 review F2): fatal: and error: lines outrank ssh:, which outranks
// a meaningful remote: line, and remote: progress lines are skipped.
func TestFirstErrorLine(t *testing.T) {
	rows := []struct {
		name   string
		stderr string
		want   string
	}{
		{
			"fetch shape: progress first",
			"remote: Enumerating objects: 4, done.\nremote: Counting objects: 100% (4/4), done.\nremote: Compressing objects: 100% (4/4), done.\nerror: RPC failed; curl 18 transfer closed with outstanding read data remaining\nfatal: early EOF",
			"error: RPC failed; curl 18 transfer closed with outstanding read data remaining",
		},
		{
			"clone shape: cloning line and progress first",
			"Cloning into '/data/repos/org/repo'...\nremote: Enumerating objects: 9, done.\nfatal: unable to access 'https://github.com/org/repo.git/': Could not resolve host: github.com",
			"fatal: unable to access 'https://github.com/org/repo.git/': Could not resolve host: github.com",
		},
		{
			"fatal outranks ssh and remote lines",
			"remote: Total 4 (delta 0), reused 0 (delta 0)\nssh: connect to host github.com port 22: Connection timed out\nfatal: Could not read from remote repository.",
			"fatal: Could not read from remote repository.",
		},
		{
			"a meaningful remote line beats a bare first line",
			"Fetching origin\nremote: Repository not found.",
			"remote: Repository not found.",
		},
		{
			"a meaningful remote line after a progress line, no fatal or error line",
			"remote: Enumerating objects: 3, done.\nremote: Permission to org/repo.git denied to someone.",
			"remote: Permission to org/repo.git denied to someone.",
		},
		{"progress only falls back to the first line", "remote: Enumerating objects: 4, done.", "remote: Enumerating objects: 4, done."},
		{"empty stderr falls back to the error", "", "exit status 128"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := firstErrorLine(row.stderr, errors.New("exit status 128")); got != row.want {
				t.Fatalf("firstErrorLine() = %q, want %q", got, row.want)
			}
		})
	}
}
