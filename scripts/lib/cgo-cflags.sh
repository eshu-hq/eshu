#!/usr/bin/env bash
#
# cgo-cflags.sh - the one place local scripts learn the C dialect Eshu's cgo
# packages must be compiled with (#9066).
#
# Why: github.com/alexaandru/go-sitter-forest/perl (v1.9.9, the newest
# release) ships its own bsearch.h that defines `void *bsearch(...)`. On
# glibc 2.43 with GCC 15, whose default dialect is C23, <stdlib.h> defines
# `bsearch` as a _Generic macro, which rewrites that definition into invalid C
# ("bsearch.h:72:7: error: expected identifier or '(' before '_Generic'").
# Compiling the cgo packages as gnu17 avoids the macro. Only a cold Go build
# cache hits this, because a warm cache skips the compile.
#
# REMOVE THIS FILE'S flag (and the matching CGO_CFLAGS lines in
# .github/workflows/*.yml, the Makefile, and docs/public/reference/
# local-testing.md) once go-sitter-forest/perl ships a release that builds
# under C23 and go/go.mod pins it. scripts/verify-cgo-cflags-guard.sh keeps the
# set of places in step until then.
#
# Go's own default for CGO_CFLAGS is "-O2 -g". Setting CGO_CFLAGS replaces that
# default instead of adding to it, so this keeps "-O2 -g" when the caller set
# nothing: without it every tree-sitter grammar would compile unoptimized.
#
# Usage (bash 3.2 compatible):
#   source "<repo>/scripts/lib/cgo-cflags.sh"
#   eshu_export_cgo_cflags
#   eshu_cgo_cflags            # prints the value without exporting it

# eshu_cgo_cflags prints the CGO_CFLAGS value to use. It appends -std=gnu17 to
# the caller's CGO_CFLAGS (or to Go's "-O2 -g" default when unset or empty) and
# never clobbers it. A caller that already chose a C dialect with any -std=
# keeps that choice, and the result is idempotent.
eshu_cgo_cflags() {
	local base="${CGO_CFLAGS:-}"
	if [ -z "${base}" ]; then
		base="-O2 -g"
	fi
	case " ${base} " in
	*" -std="*)
		printf '%s' "${base}"
		;;
	*)
		printf '%s -std=gnu17' "${base}"
		;;
	esac
}

# eshu_export_cgo_cflags exports CGO_CFLAGS with eshu_cgo_cflags's value.
eshu_export_cgo_cflags() {
	local value
	value="$(eshu_cgo_cflags)"
	CGO_CFLAGS="${value}"
	export CGO_CFLAGS
}
