#!/usr/bin/env bash
#
# benchstat-clean.sh — B-2 (#3795) helper. Filter raw `go test -bench` output to
# clean benchstat input.
#
# The code under benchmark logs to stdout (Go's standard logger ends up on the
# same stream as the benchmark result lines here), so the raw output is bloated
# with log lines and — when a log write lands between a benchmark's name and its
# result — corrupted benchmark lines that benchstat cannot parse. Keep only the
# metadata headers and well-formed benchmark result lines (name, iteration count,
# ns/op); drop everything else. A benchmark whose every sample was corrupted by
# logging simply drops out of the comparison rather than poisoning it.

benchstat_clean_filter() {
	# $1 = raw input file, $2 = cleaned output file
	rg '^(goos|goarch|pkg|cpu):|^Benchmark[^[:space:]]+[[:space:]]+[0-9]+[[:space:]]+[0-9.]+ ns/op' \
		"$1" >"$2"
}
