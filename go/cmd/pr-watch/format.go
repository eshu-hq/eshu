// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// maxFieldBytes caps free-text fields such as review bodies so one event stays
// one readable line. The CI log tail is capped separately by maxTailBytes.
const maxFieldBytes = 240

// emit writes one event line: a UTC timestamp, the kind, the PR number (when
// pr > 0), then key=value pairs. kv is a flat key, value, key, value list.
// Values that contain whitespace, quotes, or equals signs are quoted, and
// embedded newlines never survive, so every event is exactly one line.
func (w *Watcher) emit(kind string, pr int, kv ...string) {
	var b strings.Builder
	b.WriteString(w.now().UTC().Format("2006-01-02T15:04:05Z"))
	b.WriteByte(' ')
	b.WriteString(kind)
	if pr > 0 {
		fmt.Fprintf(&b, " pr=%d", pr)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		b.WriteByte(' ')
		b.WriteString(kv[i])
		b.WriteByte('=')
		b.WriteString(formatValue(kv[i+1]))
	}
	b.WriteByte('\n')
	_, _ = w.out.Write([]byte(b.String()))
}

// formatValue renders one field value for an event line.
func formatValue(v string) string {
	v = oneLine(v)
	if v == "" {
		return `""`
	}
	if strings.IndexFunc(v, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '=' || !unicode.IsPrint(r)
	}) >= 0 {
		return strconv.Quote(v)
	}
	return v
}

// oneLine collapses all whitespace runs, including newlines, to single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip collapses whitespace like oneLine and truncates the result to
// maxFieldBytes on a rune boundary. Free text from other people (review
// bodies, error messages) goes through clip; the CI log tail has its own cap.
func clip(s string) string {
	s = oneLine(s)
	if len(s) <= maxFieldBytes {
		return s
	}
	cut := maxFieldBytes
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// short returns the first 12 characters of a commit SHA.
func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
