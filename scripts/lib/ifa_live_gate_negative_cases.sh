#!/usr/bin/env bash
# shellcheck shell=bash disable=SC2034
# Negative controls for the real Ifá live-gate registry matcher. Split out of
# scripts/lib/ifa_live_gate_selector_cases.sh once that file reached 488 of the
# blocking 500-line cap, and sourced back into it so the consuming loops in
# scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh see both arrays
# declared here.
#
# This split is the thing #6200 is about, done deliberately instead of by
# accident. Before this change the four Ifá gates named 71 scripts/lib/ files
# one at a time and carried three globs between them, none reaching this file,
# so a split here would have left
# the new half selecting nothing while the original filename kept its entry --
# nothing dangling, no drift check firing, and the table that proves the live
# gates select correctly half-invisible to them. Both halves are covered by
# `scripts/lib/ifa_live_gate_*.sh` on ifa-determinism and ifa-fault-injection,
# and the file below is a positive control for exactly that.
#
# Every array in the sibling file answers "does this path still select the gate
# it must". None of them can answer the opposite question, and that question got
# sharper the moment per-file trigger lists were replaced by globs: an over-wide
# glob fails no assertion anywhere, it just quietly arms a four-shard Docker
# matrix and a three-cell determinism matrix on edits they cannot observe, and
# the bill lands on whoever is waiting for CI.
#
# Each path below is a real file that MUST NOT select either live gate, chosen
# so that a specific plausible over-widening trips it:
#
#   docs/public/architecture.md            a bare '**' or 'docs/**'
#   sdk/go/factschema/{aws,azure}/v1/...   'sdk/go/factschema/**' -- these
#                                          sibling packages are deliberately
#                                          OUT: Go package scope keeps their
#                                          helpers away from the decode path
#                                          the driven cassettes take, and no
#                                          cassette carries their facts
#   go/internal/{parser,query,telemetry,mcp}/...
#                                          'go/**' or 'go/internal/**'
#   go/internal/ifa/{saturation,throughput}/...
#                                          'go/internal/ifa/**' where the
#                                          package root 'go/internal/ifa/*.go'
#                                          is what was meant. Those two are
#                                          sibling PACKAGES that neither
#                                          go/cmd/ifa nor the ifa root package
#                                          imports -- they are the
#                                          ifa-load-saturation gate's
#                                          `go test -race` surface, and
#                                          nothing the live Docker lanes run
#                                          links them.
#
# The four scripts/lib/ entries are the #6200 half. Until this change the two
# live gates had NO glob anywhere under scripts/lib/, so no negative control
# for that directory was possible -- there was nothing to over-widen. There is
# now, and these are the four shapes a hand slips into:
#
#   scripts/lib/live-gate-lock.sh          'scripts/lib/*live*' or
#                                          'scripts/lib/*gate*' -- the shared
#                                          host-port mutex every live gate
#                                          takes, so it reads like part of the
#                                          surface and is not: it is generic
#                                          serialisation with no Ifá content.
#                                          Nearest miss to 'ifa_*_live*.sh'.
#   scripts/lib/test-verify-ci-gates-registry-ifa-filter-cases.sh
#                                          'scripts/lib/*ifa*' or
#                                          'scripts/lib/test-*ifa*'. It has
#                                          "ifa" in its name and belongs to the
#                                          registry mirror, not to either live
#                                          gate.
#   scripts/lib/test-precommit-go-filecap-cases.sh
#                                          'scripts/lib/test-*-cases.sh' -- the
#                                          shape 'test-ifa-*-cases.sh' collapses
#                                          to if the family segment is dropped.
#   scripts/lib/golden-corpus-fixtures.sh  'scripts/lib/**' or
#                                          'scripts/lib/*.sh', the widest form
#                                          of the fix this file documents.
#
# The consuming loop also asserts each path still EXISTS. Without that, a
# rename turns a negative control into a check of nothing, which is the same
# false-green shape as the dark triggers this issue is about.
ifa_live_gate_negative_seams=(
	'docs/public/architecture.md'
	'sdk/go/factschema/aws/v1/resource.go'
	'sdk/go/factschema/azure/v1/resource.go'
	'go/internal/parser/registry.go'
	'go/internal/query/openapi.go'
	'go/internal/telemetry/instruments.go'
	'go/internal/mcp/server.go'
	'go/internal/ifa/saturation/saturation.go'
	'go/internal/ifa/throughput/throughput.go'
	'scripts/lib/live-gate-lock.sh'
	'scripts/lib/test-verify-ci-gates-registry-ifa-filter-cases.sh'
	'scripts/lib/test-precommit-go-filecap-cases.sh'
	'scripts/lib/golden-corpus-fixtures.sh'
)

# The array above asks "selects NEITHER live gate", which is the only question
# worth asking about a path no Ifá gate should touch. It cannot ask anything
# about ifa-dead-letter-matrix, the fourth Ifá gate, and every loop in
# scripts/lib/test-ifa-determinism-registry-lockstep-cases.sh names only
# ifa-determinism and ifa-fault-injection -- so a trigger widened on the
# dead-letter gate fails no assertion anywhere in this repository.
#
# That is not hypothetical. An earlier commit on this branch gave
# ifa-dead-letter-matrix 'scripts/lib/ifa_determinism_*.sh' when the only lib
# scripts/verify-ifa-dead-letter-matrix.sh sources is
# ifa_determinism_common.sh, so editing ifa_determinism_lifecycle.sh made
# `make pre-pr` select a live Docker gate that cannot observe the file. The
# trigger is ifa_determinism_common*.sh now; this array is what keeps it there.
#
# Entries are path|required|forbidden. The path is a real file that must keep
# selecting the required gate and must never select the forbidden one, which
# is why it cannot live in the array above -- that array's question is
# "selects nothing", and this file's whole point is that it selects something.
# Naming the required gate is what stops the control from passing because the
# path went dark: "still selects SOMETHING" is not available as a weaker form,
# since no-diff-fragments and no-ai-attribution trigger on "**".
ifa_live_gate_negative_gate_seams=(
	'scripts/lib/ifa_determinism_lifecycle.sh|ifa-determinism|ifa-dead-letter-matrix'
)
