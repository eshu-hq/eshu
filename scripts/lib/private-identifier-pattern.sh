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
#   1. The QA and production environment names with one or two separators
#      between the words (a hyphen, underscore, space or tab: "-", "_", " ",
#      "--", two spaces), any case, with or without a suffix (the "-shaped" and
#      "_scale" forms). A letter or digit before the name means a longer word (a
#      "devops-prod" runbook), which is not a hit.
#   2. The same names joined into one word, in any casing, as a CamelCase or
#      upper-case identifier fragment or a sentence-case word (a Go test name,
#      a constant, "Opsqa" in prose). A preceding "v" is skipped so "DevOps..."
#      is not a hit, and a letter after the name must start a new capitalised
#      word, so a longer English word is not a hit either. One carve-out: the
#      all-lowercase joined form is a hit as a standalone word, but not in the
#      shape digits only, hyphen, name, hyphen. That is the append-only ledger
#      id and evidence file name spelling ("6704-<name>-directory-..."), which
#      cannot be edited and which a new ledger row repeats. Any other hyphenated
#      name ("deploy-<name>-api", and a letter-prefixed digit token such as
#      "s3-<name>-logs") is a hit.
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
# Class 2 is built from three arms, each at most one of the joined spellings:
#   mixed: the first word has a capital (Ops, oPs, opS); the second is any case.
#   tail:  the first word is lowercase "ops" and the second has a capital.
#   lower: both words lowercase; see the ledger carve-out above. Four
#          alternatives: no hyphen before the name; a hyphen before it that does
#          not follow a digit; a hyphen before it that follows a digit, which is
#          clean only when a hyphen also follows the name; and a hyphen before
#          it that follows a letter plus digits (s3, ec2, web01), always a hit,
#          so the carve-out covers a digit-only token and nothing else.
# After any match the next character must be a capital, a non-alphanumeric
# character, or the end of the line (after the lower arm's digit-hyphen form, a
# hyphen is also allowed: that is the ledger shape).
pi_end='(?:[A-Z]|[^A-Za-z0-9]|$)'
pi_mixed='(?:^|[^Vv])(?:O(?i:ps)|oP(?i:s)|opS)(?i:qa|prod)'"${pi_end}"
pi_tail='(?:^|[^Vv])ops(?:Qa|QA|qA|P(?i:rod)|pR(?i:od)|prO(?i:d)|proD)'"${pi_end}"
pi_lower='(?:^|[^A-Za-z0-9-])ops(?:qa|prod)'"${pi_end}"'|(?:^|[^0-9])-ops(?:qa|prod)'"${pi_end}"'|[0-9]-ops(?:qa|prod)(?:[A-Z]|[^A-Za-z0-9-]|$)|[A-Za-z][0-9]+-ops(?:qa|prod)'"${pi_end}"
# shellcheck disable=SC2034 # consumed by the scripts that source this file
PRIVATE_IDENTIFIER_PATTERN='(?i:(?:^|[^a-z0-9])ops[-_ \t]{1,2}(?:qa|prod)(?:[^a-z0-9]|$))|'"${pi_mixed}|${pi_tail}|${pi_lower}"'|(?i:(?:^|[^a-z0-9])bg-(?:prod|qa|dev)(?:[^a-z0-9]|$))|(?i:b[o]ats[-_ ]?gr[o]up)'
unset pi_end pi_mixed pi_tail pi_lower
