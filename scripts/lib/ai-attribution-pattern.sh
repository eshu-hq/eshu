# shellcheck shell=bash
# ai-attribution-pattern.sh: the one definition of what counts as AI
# attribution in a commit, diff, or PR body. Sourced by
# scripts/verify-no-ai-attribution.sh and scripts/dev/pre-enqueue-check.sh so
# the two cannot drift.
#
# Case-insensitive ERE of real AI-attribution markers. Deliberately specific so
# it matches AI attribution, not prose naming the rule and NOT a normal human
# Co-authored-by trailer: a Co-authored-by line is flagged only when it names an
# AI tool (or the Anthropic address), whether it ends in an <email> or at the end
# of the line. Plus "generated with/by <AI tool>", the Claude Code robot-emoji
# footer, and the Anthropic noreply address anywhere.
#
# This file necessarily matches its own pattern, so verify-no-ai-attribution.sh
# excludes it from content scans.
# shellcheck disable=SC2034 # consumed by the scripts that source this file
AI_ATTRIBUTION_PATTERN='co-authored-by:.*(claude|copilot|chatgpt|gpt-|cursor|gemini|codex|anthropic)(.*<|[[:space:]]*$)|generated (with|by) (\[?claude|copilot|chatgpt|gpt-|cursor|gemini|codex)|🤖 generated with|noreply@anthropic\.com'
