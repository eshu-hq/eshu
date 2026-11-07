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
// caller, the installed manifest, and the pinned tesseract engine payload
// into the activation destination, stamping the digest-pinned seed image
// into the manifest copy and failing closed on any non-digest reference.
// The engine payload lands under engine/ (bin/ + flat lib/ + tessdata/),
// with every file's SHA-256 verified against the per-platform pin set
// during a streaming copy, plus a pin manifest (pin-manifest.yaml) and the
// musl loader path file (lib-path); any failure removes the destination so
// no partial payload survives. The stdio branch (`--sdk-stdio`) serves one
// host claim from stdin: it runs the existing startup verification and the
// untouched drain over the activation artifact dir, then commits the
// drained envelopes into the SDK result only when the activation's live
// producer grants authorize every emitted kind, acking complete, unchanged,
// or a terminal grant denial. The claim path serves the embedded manifest,
// so it needs no /manifest.yaml file beside the binary; /manifest.yaml
// stays only as the seed source inside the seed image. The durable commit
// stays host-side; this binary never touches the database on either
// satellite branch.
package main
