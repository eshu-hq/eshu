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
//     caller binary plus the embedded manifest.yaml into <dir> and fails
//     closed on any non-digest reference. manifest.yaml names the
//     component identity the host admits, and Dockerfile.seed builds the
//     image that carries both files.
//   - --sdk-stdio serves one host claim from stdin and writes one SDK
//     result to stdout: the claim's generation drains through the
//     existing directory source and drain unchanged, the drained
//     envelopes project onto the SDK wire shape for the host's
//     grant-checked commit, and the ack echoes the claim verbatim. The
//     satellite opens no database and performs no grant evaluation; any
//     failure exits nonzero with no ack on stdout.
package main
