// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector-document-import drains one batch of operator-supplied,
// reviewed image artifacts through document OCR extraction and commits the
// resulting envelopes. It is disabled by default and exits quietly until an
// operator enables it with a pinned tesseract backend.
//
// The same binary serves the document-import satellite activation
// (#8879 Event-B) through two pre-telemetry branches. The seed branch
// (`collector seed --dest <dir> --image-ref <repo@sha256:…>`) copies the
// caller and the installed manifest into the activation destination,
// stamping the digest-pinned seed image into the manifest copy and failing
// closed on any non-digest reference. The stdio branch (`--sdk-stdio`)
// serves one host claim from stdin: it runs the existing startup
// verification and the untouched drain over the activation artifact dir,
// then commits the drained envelopes into the SDK result only when the
// activation's live producer grants authorize every emitted kind, acking
// complete, unchanged, or a terminal grant denial. The durable commit
// stays host-side; this binary never touches the database on either
// satellite branch.
package main
