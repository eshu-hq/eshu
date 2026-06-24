// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

// CodeInterprocEvidenceFactKind identifies one resolved cross-function value-flow
// taint finding emitted by the collector for reducer graph projection. The
// collector resolves both endpoints to their Function entity uids and emits the
// finding as a fact of this kind; the reducer projects it as a TAINT_FLOWS_TO
// edge from the source Function to the sink Function (evidence, never canonical
// truth).
const CodeInterprocEvidenceFactKind = "code_interproc_evidence"
