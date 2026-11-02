#!/usr/bin/env bash
#
# telemetry-coverage-required-names.sh — selects the table text of the X1 doc
# whose eshu_dp_* names must be registered in instruments.go (#9790). Split
# out of scripts/verify-telemetry-coverage.sh to keep that script under the
# repo's 500-line file cap.
#
# Sourced by scripts/verify-telemetry-coverage.sh; not meant to run
# standalone. Registered as a trigger of the telemetry-coverage gate in
# specs/ci-gates.d/gates-05.yaml; the `telemetry-coverage-*.sh` pattern in
# .github/static-contract-filters.yml already selects it in CI.

# telemetry_required_rows <rows-file> prints the text that must be checked.
#
# The table columns are: stage | file:line | metric | category. A row is
# "marker-first" when its metric cell (the third cell) starts with the
# No-Observability-Change: marker, with an optional opening backtick. The
# metric cell of such a row is retirement prose, so its names are exempt. For
# every other row the whole row is returned.
#
# For a marker-first row the exemption stops at the metric cell: its category
# cell (the last cell) is still returned, so a name there must be registered.
# Two rows glued onto one line (the `... | cat || stage | ...` shape) are
# split first, so a row separator ends the exemption of the row before it.
# A metric cell may contain stray pipes; reading only the last cell keeps
# those fragments out of the required set. A marker-first row with no
# category cell contributes nothing.
telemetry_required_rows() {
  awk '
    {
      gsub(/\|\|/, "|\n|")
      count = split($0, rows, "\n")
      for (i = 1; i <= count; i++) {
        row = rows[i]
        if (row !~ /^\|[^|]*\|[^|]*\|[ \t]*`?No-Observability-Change:/) {
          print row
          continue
        }
        cells = split(row, field, "|")
        if (field[cells] ~ /^[ \t]*$/) {
          cells--
        }
        if (cells >= 5) {
          print field[cells]
        }
      }
    }
  ' "$1"
}
