#!/usr/bin/env bash
#
# eval-media-wer.sh — WER accuracy eval for the whisper.cpp media transcript
# engine (#10073, plan /tmp/eshu-8879-accuracy-eval-plan.md).
#
# Thin layer over scripts/verify-media-transcript-sandbox.sh: it runs the
# same pinned proof (same Dockerfile, same model digest, same --network
# none sandbox profile) with ESHU_WER_EVAL=1, which un-skips
# TestSandboxWEREval (matched by the proof's -test.run '^TestSandbox').
# The test scores the synthetic quiet fixture plus the deterministic ~10 dB
# noisy mix baked into scripts/lib/media-transcript-sandbox/Dockerfile, and
# picks up the recorded-speech corpus automatically when mounted at
# ESHU_WHISPERCPP_RECORDED_DIR (*.wav with sibling *.txt ground truths).
#
# Output: MEDIA_WER_EVIDENCE JSON lines, one per fixture
# {fixture, level, ref_words, s, d, i, wer, wer_raw, redacted, canary_leak,
# wall_seconds}. All WER rows are report-only (base.en mishears synthetic
# speech; recorded thresholds ratify after banding). The canary safety
# invariant is gate-blocking: a canary-bearing transcript that reaches the
# facts fails the run.
#
# BLOCKER (recorded on #10073, not synthesized here): the 10-20 utterance
# real recorded-speech corpus (quiet + 10 dB, 2-3 speakers, consent on
# file). Until it lands, the recorded rows log as absent and only the
# synthetic harness rows are scored. Never substitute espeak output and
# call it recorded speech.
#
# Usage: scripts/eval-media-wer.sh [RECORDED_DIR]
#   RECORDED_DIR is mounted read-only into the eval container at
#   /opt/whisper/recorded when set. Requires docker with BuildKit; the
#   proof run itself has no network.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
log_out="$(mktemp)"
trap 'rm -f "${log_out}"' EXIT

recorded_dir="${1:-${ESHU_WHISPERCPP_RECORDED_DIR:-}}"
if [[ -n "${recorded_dir}" ]]; then
	[[ -d "${recorded_dir}" ]] || { printf 'eval-media-wer: recorded dir not found: %s\n' "${recorded_dir}" >&2; exit 2; }
	export ESHU_WHISPERCPP_RECORDED_DIR="${recorded_dir}"
fi

ESHU_WER_EVAL=1 bash "${repo_root}/scripts/verify-media-transcript-sandbox.sh" 2>&1 | tee "${log_out}"

rg -q 'MEDIA_WER_EVIDENCE' "${log_out}" || {
	printf 'eval-media-wer: no MEDIA_WER_EVIDENCE lines emitted\n' >&2
	exit 1
}
