// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import "context"

// CommitDispositionKind names what a commit that returned nil did with the
// generation. A nil error means only that the claim may complete: a committer
// can roll a generation back on purpose and still return nil, so that no
// collector starts failing claims over a refusal (#10037).
type CommitDispositionKind string

const (
	// CommitDispositionCommitted means the generation is durably stored. It is
	// the default: a committer that reports nothing committed.
	CommitDispositionCommitted CommitDispositionKind = "committed"
	// CommitDispositionRefused means the committer stored nothing because a
	// guard rejected the generation, so the scope keeps its prior generation.
	CommitDispositionRefused CommitDispositionKind = "refused"
	// CommitDispositionUnchangedSkip means the committer stored nothing because
	// the scope already holds this content, or this generation was already
	// finalized. Nothing the source reported was lost.
	CommitDispositionUnchangedSkip CommitDispositionKind = "unchanged_skip"
)

// CommitRefusal says why a committer refused a generation. Both fields are
// bounded, closed sets owned by the committer, so they are safe in a status
// row, a span attribute, and a log line.
type CommitRefusal struct {
	// Reason is the closed refusal class, for example
	// destructive_successor_refused or repository_retiring.
	Reason string
	// Rule is the guard arm that decided, for example collector_ratio. It is
	// empty when the refusal has no rule.
	Rule string
}

// CommitDisposition is the per-commit result box a ClaimedService or Service puts
// on the commit context. The committer marks it through MarkCommitRefused or
// MarkCommitUnchangedSkip, and the caller reads it after the commit returns
// nil. It travels on the context so the Committer interfaces and their many
// implementations stay unchanged. On a refusal, Service skips dead-letter replay
// completion, the committed-fact count, and its drain latches; observers and the
// run outcome belong to ClaimedService.
//
// One box serves one commit on one goroutine, so it holds no lock: the commit
// runs synchronously and the caller reads the box only after it returns. Its
// read methods accept a nil receiver.
type CommitDisposition struct {
	kind    CommitDispositionKind
	refusal CommitRefusal
}

// commitDispositionKey keys the box in the commit context.
type commitDispositionKey struct{}

// WithCommitDisposition returns a context carrying a fresh box and the box
// itself for the caller to read after the commit returns.
func WithCommitDisposition(ctx context.Context) (context.Context, *CommitDisposition) {
	box := &CommitDisposition{kind: CommitDispositionCommitted}
	return context.WithValue(ctx, commitDispositionKey{}, box), box
}

// MarkCommitRefused records a refusal on the box ctx carries. A commit with no
// box records nothing.
func MarkCommitRefused(ctx context.Context, refusal CommitRefusal) {
	if box, _ := ctx.Value(commitDispositionKey{}).(*CommitDisposition); box != nil {
		box.kind = CommitDispositionRefused
		box.refusal = refusal
	}
}

// MarkCommitUnchangedSkip records an unchanged skip on the box ctx carries. A
// commit with no box records nothing.
func MarkCommitUnchangedSkip(ctx context.Context) {
	if box, _ := ctx.Value(commitDispositionKey{}).(*CommitDisposition); box != nil {
		box.kind = CommitDispositionUnchangedSkip
		box.refusal = CommitRefusal{}
	}
}

// commitRefused reports whether ctx carries a box marked refused. A commit with
// no box reads as committed.
func commitRefused(ctx context.Context) bool {
	box, _ := ctx.Value(commitDispositionKey{}).(*CommitDisposition)
	return box != nil && box.kind == CommitDispositionRefused
}

// Kind returns the recorded disposition. A nil box reads as committed.
func (d *CommitDisposition) Kind() CommitDispositionKind {
	if d == nil {
		return CommitDispositionCommitted
	}
	return d.kind
}

// Refusal returns the recorded refusal and whether the commit was refused.
func (d *CommitDisposition) Refusal() (CommitRefusal, bool) {
	if d == nil || d.kind != CommitDispositionRefused {
		return CommitRefusal{}, false
	}
	return d.refusal, true
}
