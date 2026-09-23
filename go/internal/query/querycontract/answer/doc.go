// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package answer holds the answer-packet and answer-metadata contract: the
// evidence-backed response plan (AnswerPacket, built by NewAnswerPacket), the
// AnswerTruthClass mapping ClassifyAnswerTruth derives from a TruthEnvelope,
// the additive answer_metadata companion (AnswerMetadata and its Attach, Build
// and FromData helpers), and the code-topic and service-story companion
// builders that attach a packet to a response body.
//
// ClassifyAnswerTruth never upgrades a page read from no backend: an envelope
// whose basis is no_backend_read classifies as fallback whatever its level.
// NewAnswerPacket drops the proposed summary when the response envelope is nil
// or carries an error.
//
// It imports its parent querycontract for TruthEnvelope, AnswerTruthClass, the
// freshness types and the row helpers, and querycontract/evidence for citation
// handles. The parent does not import it back.
package answer
