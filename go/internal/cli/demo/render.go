// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/cli/firstrun"
)

// Envelope is the canonical `{data, truth, error}` envelope, matching the
// first-run contract so the TTFA harness reads one shape across both commands.
// The error object is firstrun.EnvelopeError itself, not a mirror, so the two
// envelopes cannot drift apart.
type Envelope struct {
	Data  Result                  `json:"data"`
	Truth map[string]any          `json:"truth"`
	Error *firstrun.EnvelopeError `json:"error"`
}

// EnvelopeFor renders a result (or a failure) into the shared envelope.
// The answer's truth labels are lifted to the envelope's Truth field so a
// consumer never has to reach into Data to judge provenance.
func EnvelopeFor(res Result, err error) Envelope {
	env := Envelope{Data: res, Truth: res.FirstAnswer.Truth}
	if env.Truth == nil {
		env.Truth = map[string]any{}
	}
	if err != nil {
		env.Error = &firstrun.EnvelopeError{Message: err.Error()}
	}
	return env
}

// WriteJSON emits the envelope with a trailing newline so shell pipelines
// and `jq` behave.
//
// The encoder error goes straight back to the cobra wrapper, which prints it
// as the command's failure. go/cmd/eshu is exempt from wrapcheck and this
// package is not, so wrapping here would add a prefix to text an operator
// reads on a broken stdout that they did not see before this package existed.
//
//nolint:wrapcheck // wrapping would change operator-visible failure text
func WriteJSON(w io.Writer, env Envelope) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// PrintSuccess renders the human path: what was proven, then what to ask
// next. The guided questions are the point of the command.
func PrintSuccess(w io.Writer, res Result) {
	_, _ = fmt.Fprintf(w, "Demo stack %q is up and indexed in %s.\n\n", res.Project,
		(time.Duration(res.TotalMillis) * time.Millisecond).Round(time.Second))
	_, _ = fmt.Fprintf(w, "First answer\n  Q: %s\n  A: %s\n", res.FirstAnswer.Question, res.FirstAnswer.Answer)
	if len(res.FirstAnswer.Truth) > 0 {
		_, _ = fmt.Fprintf(w, "  Truth: %s\n", formatTruth(res.FirstAnswer.Truth))
	}
	printGuidedPath(w)
	_, _ = fmt.Fprintf(w, "\nWhen you are done: eshu demo down --project %s\n", res.Project)
}

// printGuidedPath prints the remaining manifest questions with the command
// that actually answers each one.
//
// Generated from the manifest, never hand-written: the questions and their
// callable surfaces are declared there, and a list maintained beside it drifts
// into naming services the corpus does not contain.
func printGuidedPath(w io.Writer) {
	m, err := LoadManifest(ManifestPath)
	if err != nil || len(m.Questions) < 2 {
		// Saying nothing reads as "there is nothing more to ask", so the
		// operator never learns the section was dropped. Name the manifest:
		// it is the one fact that makes this actionable.
		_, _ = fmt.Fprintf(w, "\nGuided questions unavailable: could not read %s\n", ManifestPath)
		return
	}
	_, _ = fmt.Fprintf(w, "\nAsk these next:\n")
	n := 2
	for _, q := range m.Questions[1:] {
		runnable := q.RunnableForm()
		if runnable == "" {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %d. %s\n     %s\n", n, collapseWhitespace(q.Question), runnable)
		n++
	}
}

// collapseWhitespace flattens a manifest question's folded YAML text onto
// one line.
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// formatTruth renders truth labels deterministically so two runs of the
// same stack print the same line.
func formatTruth(truth map[string]any) string {
	keys := make([]string, 0, len(truth))
	for k := range truth {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, truth[k]))
	}
	return strings.Join(parts, " ")
}

// AskQuestion executes the manifest's first question against the running
// demo stack.
//
// It resolves the surface the manifest declares rather than calling a general
// query route, because no such route exists. An earlier draft invented
// GET /api/v0/query; the manifest is the acceptance oracle precisely so
// sibling issues do not do that.
func AskQuestion(ctx context.Context, apiBase, mcpBase, apiKey string) (Answer, error) {
	m, err := LoadManifest(ManifestPath)
	if err != nil {
		return Answer{}, err
	}
	return ExecuteQuestion(ctx, apiBase, mcpBase, apiKey, m.Questions[0])
}
