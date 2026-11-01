// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package tesseract is the general-purpose, out-of-process OCR backend
// behind ocr.Engine.
//
// Engine runs a pinned tesseract 5.x binary (LSTM engine, --oem 1) once per
// image as a bounded subprocess. It checks the image before exec (byte cap,
// PNG/JPEG/GIF/WebP magic, header dimensions, pixel and decoded-bytes
// caps). It verifies the binary, every pinned runtime library, and
// eng.traineddata against SHA-256 digests at construction, confirms the
// binary reports the configured release, and re-checks file identity on
// every call. It runs the child with no shell,
// no PATH, a scrubbed environment (OMP_THREAD_LIMIT=1), image bytes on
// stdin, a private 0700 working directory removed on every exit path,
// capped stdout and stderr, RLIMIT_AS, and a deadline. Any failure returns a
// classified *Error with an Outcome and no regions; error text never
// includes subprocess output.
//
// Output is tesseract TSV grouped into line regions. Lines, not words, are
// the region unit so ocr.Extract redacts a "password: <value>" line as one
// unit. Engine.Extract wraps ocr.Extract unchanged and records extraction
// telemetry; redaction stays in ocr.Extract.
//
// The package does not choose the sandbox. Callers must run the process
// inside the reviewed profile (--network none, --read-only, --cap-drop ALL,
// non-root, memory and pids limits); scripts/prove-ocr-sandbox.sh is the
// sandbox proof. No caller is wired in this package, and the engine does
// not bound concurrency: each call is one process.
package tesseract
