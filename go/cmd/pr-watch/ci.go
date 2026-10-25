// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// maxTailBytes caps a log tail so a failure line stays readable.
const maxTailBytes = 1200

var (
	ansiSequence = regexp.MustCompile(`\x1b\[[0-9;?]*[\x20-\x2f]*[\x40-\x7e]`)
	logTimestamp = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T[\d:.]+Z ?`)
)

// tailLog reduces a raw Actions job log to its last n useful lines, joined
// with " | ". It removes ANSI escapes and per-line timestamps and drops blank
// lines. When the log holds an ##[error] annotation, the tail ends at the last
// one, so post-job cleanup lines do not push the real failure out of view.
func tailLog(raw string, n int) string {
	var lines []string
	for _, line := range strings.Split(raw, "\n") {
		line = ansiSequence.ReplaceAllString(strings.TrimRight(line, "\r"), "")
		line = strings.TrimSpace(logTimestamp.ReplaceAllString(line, ""))
		if line != "" {
			lines = append(lines, line)
		}
	}
	end := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "##[error]") {
			end = i + 1
			break
		}
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	tail := strings.Join(lines[start:end], " | ")
	if len(tail) > maxTailBytes {
		cut := len(tail) - maxTailBytes
		for cut < len(tail) && !utf8.RuneStart(tail[cut]) {
			cut++
		}
		tail = "..." + tail[cut:]
	}
	return tail
}

// failureTail fetches a failed Actions job's log and returns its tail. `gh run
// view --log-failed` refuses while the run is still in progress, so this reads
// the job's logs endpoint directly. A missing or unreadable log degrades to a
// marker; the failure is still reported.
func (w *Watcher) failureTail(ctx context.Context, token string, c Check) (string, bool) {
	id, ok := jobID(c.URL)
	if !ok {
		return "", false
	}
	raw, err := w.gh(ctx, token, "api", "--allow-escape-sequences",
		fmt.Sprintf("repos/%s/actions/jobs/%s/logs", w.cfg.Repo, id))
	if err != nil {
		return "(log unavailable: " + clip(err.Error()) + ")", true
	}
	tail := tailLog(string(raw), w.cfg.LogLines)
	if tail == "" {
		return "(log is empty)", true
	}
	return tail, true
}
