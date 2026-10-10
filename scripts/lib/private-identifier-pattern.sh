# shellcheck shell=bash
# private-identifier-pattern.sh: the one definition of which environment and
# organization identifiers must not appear in added repository content, commit
# messages, PR bodies, or issue drafts. Sourced by
# scripts/verify-no-private-identifiers.sh, scripts/dev/pre-enqueue-check.sh and
# .agents/skills/eshu-publish/scripts/check-shape.sh so the three cannot drift.
#
# The value is a Rust-regex alternation for ripgrep. Case sensitivity is set
# inside the pattern, so call `rg -e "$PRIVATE_IDENTIFIER_PATTERN"` without -i.
# Four classes:
#   1. The QA and production environment names with a hyphen, underscore or
#      single space between the words, any case, with or without a suffix (the
#      "-shaped" and "_scale" forms). A letter or digit before the name means a
#      longer word (a "devops-prod" runbook), which is not a hit.
#   2. The same names as a CamelCase identifier fragment (a Go test name):
#      OpsQa, OpsQA, opsQa, opsQA, OpsProd, opsProd, with PROD accepted too. A
#      preceding "v" is skipped so "DevOps..." is not a hit. The joined all-
#      lowercase form is not matched: it is the append-only ledger citation
#      spelling, which cannot be edited.
#   3. The employer environment prefix (bg-prod, bg-qa, bg-dev).
#   4. The organization name, as one word or with a separator.
#
# Deliberately NOT matched, by measurement (issue #7803): the generic
# `r_<8 hex>` repository-id shape and 12-digit numbers. Both are dominated by
# synthetic and golden-corpus values, so a shape guard would block legitimate
# fixtures. Real hashed ids stay on the review pass and on the private denylist
# named by ESHU_PRIVATE_IDENTIFIER_FILE (see verify-no-private-identifiers.sh).
#
# The environment tokens already appear in public scripts in this tree. The
# organization arm is written with single-character classes so the name never
# appears as a contiguous string in the repository, including here.
# verify-no-private-identifiers.sh excludes this file from its content scan so
# the gate never flags its own definition.
# shellcheck disable=SC2034 # consumed by the scripts that source this file
PRIVATE_IDENTIFIER_PATTERN='(?i:(?:^|[^a-z0-9])ops[-_ ](?:qa|prod)(?:[^a-z0-9]|$))|(?:^|[^Vv])[Oo]ps(?:Qa|QA|Prod|PROD)(?:[A-Z]|[^A-Za-z0-9]|$)|(?i:(?:^|[^a-z0-9])bg-(?:prod|qa|dev)(?:[^a-z0-9]|$))|(?i:b[o]ats[-_ ]?gr[o]up)'
