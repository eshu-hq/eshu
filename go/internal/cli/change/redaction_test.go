// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// canary is a sentinel that appears in no fixture, no format string, and no
// key name in this package. It is planted inside a VALUE -- a path segment --
// rather than under a key that looks sensitive, because a value is where the
// leaks on this epic actually lived.
const canary = "C4N4RY6059XYZ"

// canaryPrefixes are the characters placed immediately before the sentinel.
// A scan that only ever sees the sentinel at a segment boundary proves nothing
// about a sentinel that arrives glued to a letter, a quote, or an @ -- and the
// composed strings this package writes put values next to all of these.
var canaryPrefixes = map[string]string{
	"segment start": "",
	"letter":        "a",
	"space":         " ",
	"at":            "@",
	"quote":         "\"",
	"colon":         ":",
	"dot":           ".",
	"dash":          "-",
	"slash":         "/",
}

// rendering is one place bytes leave this package.
type rendering struct {
	name string
	body string
}

// renderEverything drives every output path this package has, for one Options
// and one Envelope, and returns the bytes each produced.
//
// The four in-memory renderings are the whole output surface: FinishImpact and
// FinishPlan, each in summary and JSON mode. The fifth writes through an
// os.File so the assertion covers bytes that landed on disk. This package
// never opens a file itself -- go/cmd/eshu hands it cmd.OutOrStdout() -- but an
// operator redirecting that stream to a file is the ordinary case, and a check
// that only ever looks at a bytes.Buffer would not have covered it.
func renderEverything(t *testing.T, opts Options, envelope Envelope) []rendering {
	t.Helper()

	out := []rendering{}
	for _, mode := range []struct {
		name string
		json bool
	}{{name: "summary", json: false}, {name: "json", json: true}} {
		modeOpts := opts
		modeOpts.JSON = mode.json

		impact := &bytes.Buffer{}
		_ = FinishImpact(impact, modeOpts, envelope, ClassifyImpact(envelope))
		out = append(out, rendering{name: "impact " + mode.name, body: impact.String()})

		plan := &bytes.Buffer{}
		_ = FinishPlan(plan, modeOpts, envelope, ClassifyPlan(envelope))
		out = append(out, rendering{name: "plan " + mode.name, body: plan.String()})
	}

	path := filepath.Join(t.TempDir(), "impact.out")
	file, err := os.Create(path) // #nosec G304 -- path is built from t.TempDir()
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	if err := FinishImpact(file, opts, envelope, ClassifyImpact(envelope)); err != nil {
		t.Fatalf("FinishImpact to file: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close %s: %v", path, err)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- same temp path just written above
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out = append(out, rendering{name: "impact bytes on disk", body: string(data)})
	return out
}

// optionsCarrying builds an Options whose changed-file set carries the
// sentinel, through all three entry points that produce FileChange rows, so a
// leak via any of them is covered.
//
// Every string field of Options carries it, not a selection. The scope of this
// check is "nothing an operator typed reaches output", and a field left
// unplanted is a field the check silently exempts. DeveloperIntent is the one
// that would hurt most: it is free text from --intent, so unlike a repo ID or
// a ref it has no shape at all, and a project name or an unannounced customer
// lands in it as easily as anything else. The int fields are left alone --
// MaxDepth, Limit, and Offset cannot carry a sentinel and cannot carry a name.
func optionsCarrying(marked string) Options {
	fromGit := ParseNameStatusDiff("M\t" + marked + "\nR100\t" + marked + "-old\t" + marked + "-new\n")
	explicit := ModifiedFiles([]string{marked})
	changes := append(fromGit, explicit...)
	return Options{
		RepoID:          "repo:" + marked,
		DeveloperIntent: "roll out " + marked + " before the announcement",
		RepoPath:        "/home/dev/" + marked,
		BaseRef:         "refs/heads/" + marked,
		HeadRef:         marked,
		Target:          "service:" + marked,
		TargetType:      "service-" + marked,
		ServiceName:     marked,
		WorkloadID:      "workload:" + marked,
		ResourceID:      "resource:" + marked,
		ModuleID:        "module:" + marked,
		Topic:           marked,
		Environment:     "prod-" + marked,
		Changes:         changes,
		ChangedPaths:    ChangedPaths(changes),
		MaxDepth:        4,
		Limit:           25,
	}
}

// TestOptionsCarryingPlantsEveryStringField keeps optionsCarrying honest.
//
// The absence check below is only as wide as the input it is given. A string
// field nobody planted is a field that check exempts, and it exempts it
// silently -- the run stays green and the coverage shrinks. Reflection is what
// notices, so a field added to Options later fails here until it is planted.
//
// That accounts for 13 of Options' 19 fields. The two slice fields are covered
// by the changed-file assertions in TestOperatorInputNeverReachesRendering, and
// the three int fields and the JSON bool can carry neither a sentinel nor a
// name.
func TestOptionsCarryingPlantsEveryStringField(t *testing.T) {
	t.Parallel()

	opts := optionsCarrying("go/internal/" + canary + "/handler.go")
	value := reflect.ValueOf(opts)
	planted := 0
	for i := range value.NumField() {
		field := value.Type().Field(i)
		if field.Type.Kind() != reflect.String {
			continue
		}
		if !strings.Contains(value.Field(i).String(), canary) {
			t.Fatalf("Options.%s carries no sentinel; an unplanted field is one the absence check silently exempts", field.Name)
		}
		planted++
	}
	if planted != 13 {
		t.Fatalf("planted %d string fields, want 13; plant the new field in optionsCarrying before moving this number", planted)
	}
}

// TestOperatorInputNeverReachesRendering is the negative direction: a repo
// diff's file paths, the local repository path, the refs, and the selectors an
// operator typed all sit in Options, and none of them may appear in anything
// this package prints.
//
// Why this matters even though none of it is a credential: a path from a real
// diff routinely names a customer, an acquisition, or an unannounced project.
// The question here is disclosure of those names, not secret material.
//
// The envelope in this test is a clean API response that does not echo any
// path back. That is the condition being checked -- the renderers must not
// reintroduce Options into output on their own. What the API chooses to put in
// `data` is the API's contract, and JSON mode passes it through by design;
// TestCanaryPositiveControl covers that direction.
func TestOperatorInputNeverReachesRendering(t *testing.T) {
	t.Parallel()

	for name, prefix := range canaryPrefixes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			marked := "go/internal/" + prefix + canary + "/handler.go"
			opts := optionsCarrying(marked)

			// The sentinel really is in the input, or this test would pass by
			// carrying nothing.
			if !strings.Contains(strings.Join(opts.ChangedPaths, "\n"), canary) {
				t.Fatalf("changed paths do not carry the sentinel: %#v", opts.ChangedPaths)
			}

			for _, r := range renderEverything(t, opts, impactEnvelope()) {
				if strings.Contains(r.body, canary) {
					t.Fatalf("%s leaked operator input: %q", r.name, r.body)
				}
			}
		})
	}
}

// TestCanaryPositiveControl is the control run. It plants the same sentinel in
// the places the API controls -- the freshness state, a coverage state, a
// next-call reason, an action title, and the envelope error message -- and
// requires every rendering to show it.
//
// Without this, TestOperatorInputNeverReachesRendering proves nothing: a scan
// that reads an empty string, or the wrong buffer, reports "clean" for every
// input. Each rendering is named individually so a control that stops firing
// for one of the five is a failure rather than a silent gap.
func TestCanaryPositiveControl(t *testing.T) {
	t.Parallel()

	envelope := impactEnvelope()
	envelope.Truth = map[string]any{"freshness": map[string]any{"state": "fresh" + canary}}
	envelope.Data["coverage"] = map[string]any{"state": "partial" + canary}
	envelope.Data["recommended_next_calls"] = []any{map[string]any{"tool": "t", "reason": canary}}
	envelope.Data["actions"] = []any{map[string]any{"kind": "k", "risk": "r", "title": canary}}
	envelope.Data["bounded_next_calls"] = []any{map[string]any{"kind": "api", "target": canary}}

	renderings := renderEverything(t, Options{}, envelope)
	if len(renderings) != 5 {
		t.Fatalf("renderEverything produced %d renderings, want 5", len(renderings))
	}
	for _, r := range renderings {
		if !strings.Contains(r.body, canary) {
			t.Fatalf("control did not fire for %s; the absence check there proves nothing: %q", r.name, r.body)
		}
	}
}

// TestEnvelopeErrorMessageIsPrintedVerbatim records a carrier that is real and
// deliberately unscreened: the transport error text.
//
// A failed API call is rendered as `Pre-change impact error (code): message`,
// and for a transport failure that message is the Go error, which embeds the
// service URL the operator set with --service-url or ESHU_SERVICE_URL. That URL
// reaching the operator's own terminal is the point of the message. Go's
// net/http strips userinfo from the URL in the errors it builds, so a password
// in the URL does not survive that far, but this package does not screen the
// message and must not be described as if it does.
func TestEnvelopeErrorMessageIsPrintedVerbatim(t *testing.T) {
	t.Parallel()

	message := `Post "https://eshu.internal.` + canary + `:8443/api/v0/impact/pre-change": dial tcp: connection refused`
	envelope := EnvelopeFromTransportError(&stringError{message})
	if got, want := envelope.Error.Code, "backend_unavailable"; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}

	out := &bytes.Buffer{}
	_ = FinishImpact(out, Options{}, envelope, EnvelopeFailure(envelope.Error))
	if !strings.Contains(out.String(), canary) {
		t.Fatalf("transport error message was altered; this test documents that it is printed verbatim: %q", out.String())
	}
	if got, want := out.String(), "Pre-change impact error (backend_unavailable): "+message+"\n"; got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

type stringError struct{ message string }

func (e *stringError) Error() string { return e.message }
