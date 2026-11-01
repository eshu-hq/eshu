// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contract

// Out-of-process OCR engine (#9728). The tesseract backend under
// go/internal/collector/document/ocr/tesseract runs one sandboxed subprocess
// per image; these names let an operator follow one extraction from preflight
// through the bounded subprocess to the redacted section facts. Span
// attributes carry only bounded values (outcome, format, byte and pixel
// counts, region and redaction counts, peak RSS), never OCR text.
const (
	// SpanOCRExtract wraps one image extraction: picture preflight, the
	// engine call, and redaction into documentation facts.
	SpanOCRExtract = "ocr.extract"
	// SpanOCRRecognize wraps one bounded OCR subprocess run, from input
	// validation through process exit and output parsing.
	SpanOCRRecognize = "ocr.recognize"
)
