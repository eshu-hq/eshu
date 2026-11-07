// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector-media-import drains one batch of operator-supplied,
// reviewed audio artifacts through document media-transcript extraction and
// commits the resulting envelopes. It is disabled by default and exits quietly
// until an operator enables it on the QA tier with a pinned whisper.cpp
// backend. Only WAV reaches the engine; every other format stays off by
// default and drains as a skipped observation.
//
// The satellite surface is two extra modes selected before the default
// one-shot Postgres drain:
//
//   - seed materializes the seed payload for the seed image build:
//     collector seed --dest <dir> --image-ref <repo@sha256:…> copies the
//     caller binary, the embedded manifest.yaml, and the pinned whisper
//     engine payload into <dir> and fails closed on any non-digest
//     reference. The engine payload lands under engine/ (bin/ + models/),
//     with every file's SHA-256 verified against the per-platform pin set
//     during a streaming copy, plus a pin manifest (pin-manifest.yaml) and
//     the musl loader path file (lib-path); any failure removes the
//     destination so no partial payload survives. manifest.yaml names the
//     component identity the host admits, and Dockerfile.seed builds the
//     image that carries both files.
//   - --sdk-stdio serves one host claim from stdin and writes one SDK
//     result to stdout: the claim's generation drains through the
//     existing directory source and drain unchanged, the drained
//     envelopes project onto the SDK wire shape when the activation's live
//     producer grants authorize every emitted kind, and the ack echoes the
//     claim verbatim. A grant denial acks terminal with grant_denied; any
//     other failure exits nonzero with no ack on stdout.
package main
