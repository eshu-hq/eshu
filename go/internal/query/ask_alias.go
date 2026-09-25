// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B5 root alias shim for #6642: the ask aliases must live in package query so handler wiring, the impact shim, and the staying auth test compile unchanged.

import (
	"github.com/eshu-hq/eshu/go/internal/query/ask"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract/answer"
)

// ask_alias.go is the root alias shim for the ask family, which moved to
// internal/query/ask (#6642). The impact shim, investigation packet builders,
// handler wiring, and the staying ask auth test spell these names through
// package query, so this stays until the #6642 alias sweep.

// AskHandler serves POST /api/v0/ask. See ask.Handler.
type AskHandler = ask.Handler

// ErrNoStreaming reports an adapter without streaming support. See
// ask.ErrNoStreaming.
var ErrNoStreaming = ask.ErrNoStreaming

// AnswerTruthClass is the answer truth classification. See
// querycontract.AnswerTruthClass via ask.AnswerTruthClass.
type AnswerTruthClass = ask.AnswerTruthClass

// Answer truth-class values preserved from the root package. See ask.
const (
	AnswerTruthDeterministic       = ask.AnswerTruthDeterministic
	AnswerTruthDerived             = ask.AnswerTruthDerived
	AnswerTruthFallback            = ask.AnswerTruthFallback
	AnswerTruthSemanticObservation = ask.AnswerTruthSemanticObservation
	AnswerTruthCodeHint            = ask.AnswerTruthCodeHint
	AnswerTruthUnsupported         = ask.AnswerTruthUnsupported
)

// ClassifyAnswerTruth maps a truth envelope to a single truth class. See
// ask.ClassifyAnswerTruth.
func ClassifyAnswerTruth(truth *TruthEnvelope) AnswerTruthClass {
	return ask.ClassifyAnswerTruth(truth)
}

// appendReason appends a reason, dropping blanks and duplicates. Its home is
// ask; this forwarder keeps the investigation packet builders (which stay in
// root per their own move rows) calling the package-local name.
func appendReason(reasons []string, reason string) []string {
	return ask.AppendReason(reasons, reason)
}

// AskAnswer is the engine answer shape. See ask.AskAnswer.
type AskAnswer = ask.AskAnswer

// AskStreamEvent is one streaming emission. See ask.AskStreamEvent.
type AskStreamEvent = ask.AskStreamEvent

// AnswerPacket composes query truth into a user-ready response. See
// ask.AnswerPacket.
type AnswerPacket = ask.AnswerPacket

// AnswerPacketInput builds an AnswerPacket. See ask.AnswerPacketInput.
type AnswerPacketInput = ask.AnswerPacketInput

// NewAnswerPacket builds an AnswerPacket. See ask.NewAnswerPacket.
func NewAnswerPacket(in AnswerPacketInput) AnswerPacket {
	return ask.NewAnswerPacket(in)
}

// NewAnswerPacketFromMetadata composes an AnswerPacket from normalized
// metadata. See ask.NewAnswerPacketFromMetadata.
func NewAnswerPacketFromMetadata(in AnswerPacketInput, metadata answer.AnswerMetadata) AnswerPacket {
	return ask.NewAnswerPacketFromMetadata(in, metadata)
}
