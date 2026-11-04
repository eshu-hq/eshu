// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector-media-import drains one batch of operator-supplied,
// reviewed audio artifacts through document media-transcript extraction and
// commits the resulting envelopes. It is disabled by default and exits quietly
// until an operator enables it on the QA tier with a pinned whisper.cpp
// backend. Only WAV reaches the engine; every other format stays off by
// default and drains as a skipped observation.
package main
