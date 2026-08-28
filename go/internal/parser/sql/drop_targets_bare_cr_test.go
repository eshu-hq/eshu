// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sql

import (
	"reflect"
	"strings"
	"testing"
)

// TestSkipDropTargetCommentsEndsLineCommentAtBareCR is the #6268 regression
// for the SQL DROP-target recovery scanner. A `--` comment in a classic-Mac
// migration ends at a bare '\r'; a scan that only stops at '\n' swallows the
// whole remaining tail, so the recovered DROP targets after it are lost and
// the migration's graph truth is silently short.
func TestSkipDropTargetCommentsEndsLineCommentAtBareCR(t *testing.T) {
	t.Parallel()

	tail := ", public.users, -- between targets\r public.orgs RESTRICT;"
	targets, ok := parseDropTargetTail(tail)
	if !ok {
		t.Fatalf("parseDropTargetTail(%q) rejected a list whose comment ends at a bare CR", tail)
	}
	if got, want := targets, []recoveredDropTarget{
		{name: "public.users", offset: strings.Index(tail, "public.users")},
		{name: "public.orgs", offset: strings.Index(tail, "public.orgs")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDropTargetTail(%q) = %#v, want %#v", tail, got, want)
	}
}

// TestSkipDropTargetCommentsKeepsCRLFTailIntact is the control: a CRLF tail
// already parsed before #6268 because the '\n' terminated the comment, so it
// must stay green on both sides of the fix.
func TestSkipDropTargetCommentsKeepsCRLFTailIntact(t *testing.T) {
	t.Parallel()

	tail := ", public.users, -- between targets\r\n public.orgs RESTRICT;"
	targets, ok := parseDropTargetTail(tail)
	if !ok {
		t.Fatalf("parseDropTargetTail(%q) rejected a CRLF list", tail)
	}
	if got, want := targets, []recoveredDropTarget{
		{name: "public.users", offset: strings.Index(tail, "public.users")},
		{name: "public.orgs", offset: strings.Index(tail, "public.orgs")},
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("parseDropTargetTail(%q) = %#v, want %#v", tail, got, want)
	}
}
