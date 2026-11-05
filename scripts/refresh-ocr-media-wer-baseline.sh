#!/usr/bin/env bash
#
# refresh-ocr-media-wer-baseline.sh — recapture the WER / noisy-scan
# accuracy baseline (#10073) by running both eval harnesses and folding the
# observed *_WER_EVIDENCE rows into
# testdata/accuracy/ocr-media-wer-baseline.json.
#
# Run this on the enforcement host (consistent hardware) when the engines,
# the corpus, or the ladder recipe change, or when seeding the baseline for
# a new host. Only measured fields are updated (rows, corpus_digest,
# capture); thresholds, min_covered_items, and notes are human-curated and
# survive a recapture untouched, so a threshold change is always a separate
# reviewed edit. Review the resulting diff before committing — a baseline
# bump is a reviewed claim that the new numbers are the expected normal.
#
# Privacy: the baseline keeps aggregates and digests only (no transcripts,
# paths, or raw audio). Raw eval logs stay operator-local.
#
# Usage: scripts/refresh-ocr-media-wer-baseline.sh [RECORDED_DIR]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
baseline="${repo_root}/testdata/accuracy/ocr-media-wer-baseline.json"

die() {
	printf 'refresh-ocr-media-wer-baseline: %s\n' "$*" >&2
	exit 1
}

command -v jq >/dev/null 2>&1 || die "missing required tool: jq"
command -v rg >/dev/null 2>&1 || die "missing required tool: rg"
[[ -f "${baseline}" ]] || die "baseline not found: ${baseline}"

recorded_dir="${1:-${ESHU_WHISPERCPP_RECORDED_DIR:-}}"

log_out="$(mktemp)"
trap 'rm -f "${log_out}"' EXIT

printf 'refresh-ocr-media-wer-baseline: running OCR eval...\n' >&2
bash "${repo_root}/scripts/eval-ocr-wer.sh" 2>&1 | tee -a "${log_out}" >/dev/null
printf 'refresh-ocr-media-wer-baseline: running media eval...\n' >&2
if [[ -n "${recorded_dir}" ]]; then
	ESHU_WHISPERCPP_RECORDED_DIR="${recorded_dir}" bash "${repo_root}/scripts/eval-media-wer.sh" "${recorded_dir}" 2>&1 | tee -a "${log_out}" >/dev/null
else
	bash "${repo_root}/scripts/eval-media-wer.sh" 2>&1 | tee -a "${log_out}" >/dev/null
fi

ocr_rows="$(rg --no-filename -o 'OCR_WER_EVIDENCE (\{.*\})' -r '$1' "${log_out}" | jq -s '.' || true)"
media_rows="$(rg --no-filename -o 'MEDIA_WER_EVIDENCE (\{.*\})' -r '$1' "${log_out}" | jq -s '.' || true)"
[[ -n "${ocr_rows}" && "${ocr_rows}" != "[]" ]] || die "the OCR eval emitted no OCR_WER_EVIDENCE lines"
[[ -n "${media_rows}" && "${media_rows}" != "[]" ]] || die "the media eval emitted no MEDIA_WER_EVIDENCE lines"

corpus_digest="$(cat "${repo_root}/scripts/ocr-sandbox/Dockerfile" \
	"${repo_root}/scripts/lib/media-transcript-sandbox/Dockerfile" \
	"${repo_root}/scripts/lib/media-transcript-sandbox/recorded-corpus-script.txt" \
	| sha256sum | cut -d' ' -f1)"
commit="$(git -C "${repo_root}" rev-parse HEAD 2>/dev/null || printf 'unrecorded')"
arch="$(uname -m)"
host="$(uname -srm)"

updated="$(jq --argjson ocr "${ocr_rows}" --argjson media "${media_rows}" \
	--arg digest "${corpus_digest}" --arg commit "${commit}" --arg arch "${arch}" --arg host "${host}" '
  # unique: the eval wrappers re-print the evidence lines they scrape, so
  # the log holds each row twice; identical rows collapse to one.
  .rows = ((($ocr | map(. + {engine: "ocr"})) + ($media | map(. + {engine: "media"}))) | unique)
  | .corpus_digest = $digest
  | .capture = {
      host_class: $host,
      arch: $arch,
      commit: $commit,
      run_kind: "baseline",
      note: "Recaptured by scripts/refresh-ocr-media-wer-baseline.sh. Thresholds preserved. Re-run on the enforcement host before banding gates."
    }
' "${baseline}")" || die "failed to merge observed rows into the baseline"

printf '%s\n' "${updated}" >"${baseline}"
printf 'refresh-ocr-media-wer-baseline: updated %s — review the diff before committing\n' "${baseline}" >&2
