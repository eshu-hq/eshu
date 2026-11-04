// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Command collector-document-import drains one batch of operator-supplied,
// reviewed image artifacts through document OCR extraction and commits the
// resulting envelopes. It is disabled by default and exits quietly until an
// operator enables it with a pinned tesseract backend.
package main
