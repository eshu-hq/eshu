// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// CodeTaintEvidenceFactKind identifies one resolved intraprocedural value-flow
// taint finding emitted by the collector for reducer graph projection. The
// collector resolves each finding to its Function entity uid and emits the
// finding as a fact of this kind; the reducer projects it as a CodeTaintEvidence
// graph node attached to that Function (evidence, never canonical truth).
const CodeTaintEvidenceFactKind = "code_taint_evidence"
