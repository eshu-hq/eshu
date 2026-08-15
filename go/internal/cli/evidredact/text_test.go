// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidredact

import (
	"strings"
	"testing"
)

// sentinel is the synthetic credential every case here plants. It is one token
// with no separator in it, so a walk that removes a value cannot leave a
// recognizable half of it behind and still pass.
const sentinel = "SENTINELCREDENTIAL"

// bareCredentialCases are the URL-FREE shapes a credential arrives in when the
// first-run diagnosis carries an authentication failure back into the artifact.
//
// None of them contains "scheme://", which is the whole point: the walk that
// preceded this package split the text on absolute URLs and sent only the URL
// spans through endpoint redaction. Every span below is the OTHER half, and all
// five reached both the Markdown and the JSON artifact of
// `eshu first-run report` before urlredact.FreeText was wired into it.
var bareCredentialCases = []struct {
	name string
	text string
}{
	{
		name: "a query-shaped pair in a readiness sentence",
		text: "readiness probe rejected the request body api_key=" + sentinel + " while draining",
	},
	{
		name: "a JSON body quoted into an error cause",
		text: `api returned 401; body was {"error":"unauthorized","api_key":"` + sentinel + `"}`,
	},
	{
		name: "a header pasted into a recovery step",
		text: "retry with authorization: Bearer " + sentinel,
	},
	{
		name: "a spaced prose pair",
		text: "re-run with token = " + sentinel,
	},
	{
		name: "a percent-encoded name and separator",
		text: "cause: api%5Fkey%3D" + sentinel + " rejected",
	},
	{
		name: "a pair on a shell continuation line",
		text: "curl -H 'Authorization: \\\n" + sentinel + "'",
	},
}

// TestTextRemovesABareCredentialPairWithNoURLAround is the direct regression for
// the leak. Before urlredact.FreeText was wired into scrubURLFreeSpan every one
// of these came out byte for byte.
func TestTextRemovesABareCredentialPairWithNoURLAround(t *testing.T) {
	t.Parallel()

	for _, tc := range bareCredentialCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := Text(tc.text, nil)
			if strings.Contains(got, sentinel) {
				t.Fatalf("the credential survived Text: %q", got)
			}
			if got == tc.text {
				t.Fatalf("Text returned its input unchanged: %q", got)
			}
		})
	}
}

// TestTextKeepsAURLReadableWhenItsCredentialIsInAQueryParameter pins the span
// split from the other side.
//
// The free-text walk must NOT run over a URL. It ends a value at "?", "&" or ";"
// and replaces the pair including its name, so letting it near an endpoint would
// return "https://h/x?[redacted]&repo=demo" — an endpoint an operator cannot
// match against their own config. Endpoint already removes a credential-named
// query value, so the URL span loses nothing by being handled separately.
func TestTextKeepsAURLReadableWhenItsCredentialIsInAQueryParameter(t *testing.T) {
	t.Parallel()

	got := Text("could not reach https://h/x?token="+sentinel+"&repo=demo now", nil)
	want := "could not reach https://h/x?token=redacted&repo=demo now"
	if got != want {
		t.Fatalf("Text\n got %q\nwant %q", got, want)
	}
}

// TestTextDoesNotCorruptAURLThatContainsARawTarget pins the reason the two
// stages run on disjoint spans. A repo target can be a substring of a URL, and
// substituting inside one rewrote "https://h/z" to "https:.../h/z": unreadable
// to the operator, and no longer a URL to the stage that would have redacted it.
func TestTextDoesNotCorruptAURLThatContainsARawTarget(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"//", "/h/z"} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			got := Text("target https://h/z is up", []string{raw})
			if !strings.Contains(got, "https://h/z") {
				t.Fatalf("the raw-target stage rewrote the URL: %q", got)
			}
		})
	}
}

// TestTextSubstitutesAKnownRawTargetBeforeTheCredentialWalk is the ordering
// proof inside one span.
//
// A path with a space in it is ordinary on a developer machine. Substituting
// first collapses it to ".../demo" and the pair walk then removes the whole
// value. Scanning first ends the value at the space, so the walk removes
// "token=/Users/bob/my" and strands "repos/demo" where the substitution can no
// longer find it — the private layout Path exists to keep off the artifact,
// readable on the artifact.
func TestTextSubstitutesAKnownRawTargetBeforeTheCredentialWalk(t *testing.T) {
	t.Parallel()

	raw := "/Users/bob/my repos/demo"
	got := Text("token="+raw, []string{raw})
	if strings.Contains(got, "repos/demo") || strings.Contains(got, "bob") {
		t.Fatalf("the raw target survived the span: %q", got)
	}
	if got != "[redacted]" {
		t.Fatalf("Text\n got %q\nwant %q", got, "[redacted]")
	}
}

// TestTextSubstitutesAKnownRawTargetInProse covers the stage on its own: a
// composed sentence and the structured field it was built from must not
// disagree about the same path.
func TestTextSubstitutesAKnownRawTargetInProse(t *testing.T) {
	t.Parallel()

	got := Text("indexed /u/bob/demo already", []string{"/u/bob/demo"})
	want := "indexed .../demo already"
	if got != want {
		t.Fatalf("Text\n got %q\nwant %q", got, want)
	}
	if got == "" || strings.Contains(got, "bob") {
		t.Fatalf("the username survived: %q", got)
	}
}

// TestScrubbedTextIsAFixedPoint runs every case in this file back through the
// scrub.
//
// `eshu first-run report` re-renders an artifact from a saved envelope, so a
// second pass that found something new would mean the two runs disagree about
// what the same envelope says. The whole corpus is driven, not one case, because
// each stage has its own way of failing this: a marker containing a separator
// trips the pair walk, and a substitution whose output still contains its own
// input loops.
func TestScrubbedTextIsAFixedPoint(t *testing.T) {
	t.Parallel()

	raws := []string{"/u/bob/demo", "/Users/bob/my repos/demo", "//"}
	texts := []string{
		"could not reach https://h/x?token=" + sentinel + "&repo=demo now",
		"target https://h/z is up",
		"indexed /u/bob/demo already",
		"token=/Users/bob/my repos/demo",
		"https://alice:" + sentinel + "@h/x#access_token=" + sentinel,
	}
	for _, tc := range bareCredentialCases {
		texts = append(texts, tc.text)
	}

	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			t.Parallel()

			once := Text(text, raws)
			if again := Text(once, raws); again != once {
				t.Fatalf("Text is not a fixed point:\nfirst  %q\nsecond %q", once, again)
			}
			if strings.Contains(once, sentinel) {
				t.Fatalf("the credential survived the scrub: %q", once)
			}
		})
	}
}

// TestTruthScrubsEveryStringAtAnyDepth covers the re-emit path, where the truth
// map is whatever an operator-supplied envelope's JSON carried rather than the
// bounded label set the run itself writes.
func TestTruthScrubsEveryStringAtAnyDepth(t *testing.T) {
	t.Parallel()

	truth := map[string]any{
		"completeness": "partial",
		"nested": map[string]any{
			"note":  "password=" + sentinel,
			"items": []any{"api_key=" + sentinel, 7, true},
		},
	}
	got := Truth(truth, nil)
	if strings.Contains(renderDeep(got), sentinel) {
		t.Fatalf("a credential survived the nested walk: %v", got)
	}
	if got["completeness"] != "partial" {
		t.Fatalf("a benign label was rewritten: %v", got["completeness"])
	}
	// The caller's map stays raw: first-run's own error reporting reads it.
	if !strings.Contains(renderDeep(truth), sentinel) {
		t.Fatal("Truth mutated its input instead of copying it")
	}
}

// renderDeep flattens a decoded JSON value to one string so a test can assert a
// sentinel is absent at any depth without walking the shape by hand.
func renderDeep(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case map[string]any:
		var b strings.Builder
		for k, v := range typed {
			b.WriteString(k)
			b.WriteString(renderDeep(v))
		}
		return b.String()
	case []any:
		var b strings.Builder
		for _, v := range typed {
			b.WriteString(renderDeep(v))
		}
		return b.String()
	default:
		return ""
	}
}

// TestTextsCopiesInsteadOfMutating pins that the slice helper leaves the
// caller's data alone, because the raw result is what first-run's own human
// output still prints.
func TestTextsCopiesInsteadOfMutating(t *testing.T) {
	t.Parallel()

	input := []string{"token=" + sentinel, "fine"}
	got := Texts(input, nil)
	if input[0] != "token="+sentinel {
		t.Fatalf("Texts mutated its input: %q", input[0])
	}
	if strings.Contains(got[0], sentinel) {
		t.Fatalf("the credential survived: %q", got[0])
	}
	if got[1] != "fine" {
		t.Fatalf("a benign value was rewritten: %q", got[1])
	}
}

// TestTextLeavesTheResidueTheSpanSplitCannotReach states the limit rather than
// leaving a reader to find it. The header rule stops at the end of its span and
// a URL ends a span, so a credential written after a URL that follows its own
// key survives. Documenting it here means a future change that closes it turns
// this test red instead of passing silently.
func TestTextLeavesTheResidueTheSpanSplitCannotReach(t *testing.T) {
	t.Parallel()

	got := Text("Authorization: Bearer https://h/x "+sentinel, nil)
	if !strings.Contains(got, sentinel) {
		t.Fatalf("this limit is now closed; update the doc comment on Text and this test: %q", got)
	}
}
