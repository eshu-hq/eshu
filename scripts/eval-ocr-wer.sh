#!/usr/bin/env bash
#
# eval-ocr-wer.sh — WER / noisy-scan accuracy eval for the tesseract OCR
# engine (#10073, plan /tmp/eshu-8879-accuracy-eval-plan.md).
#
# Thin layer over scripts/prove-ocr-sandbox.sh: it runs the same pinned
# proof (same Dockerfile, same digests, same --network none sandbox
# profile) with ESHU_WER_EVAL=1, which selects TestSandboxWEREval over the
# deterministic degradation ladder baked into scripts/ocr-sandbox/Dockerfile
# (clean control + light/medium/heavy for the basic, form-stacked, and
# key-body layouts, with sibling .txt ground truths).
#
# Output: OCR_WER_EVIDENCE JSON lines, one per fixture x level
# {fixture, level, ref_words, s, d, i, wer, wer_raw, cer, extract_status,
# redacted, canary_leak, wall_seconds}. The clean control must score WER 0
# and every level must withhold the canary; degraded-level WER is
# report-only until the band is ratified from 3 baseline runs.
#
# Recorded-speech note: this script covers OCR only. The media eval
# (scripts/eval-media-wer.sh) scores synthetic fixtures until the 10-20
# utterance recorded corpus lands (BLOCKER on #10073: mic/human session).
#
# Usage: scripts/eval-ocr-wer.sh [--platform linux/arm64|linux/amd64]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log_out="$(mktemp)"
trap 'rm -f "${log_out}"' EXIT

ESHU_WER_EVAL=1 bash "${repo_root}/scripts/prove-ocr-sandbox.sh" "$@" 2>&1 | tee "${log_out}"

rg -q 'OCR_WER_EVIDENCE' "${log_out}" || {
	printf 'eval-ocr-wer: no OCR_WER_EVIDENCE lines emitted\n' >&2
	exit 1
}
