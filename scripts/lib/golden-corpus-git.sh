#!/usr/bin/env bash
#
# golden-corpus-git.sh — the one way the B-7 golden-corpus gate runs git
# against a fixture repository it is building.
#
# WHY THIS EXISTS
#
# The gate stages fixture repositories, commits them, and compares the
# resulting commit SHA against a value pinned in a cassette
# (testdata/cassettes/cicdrun/supply-chain-demo.json). That comparison is only
# meaningful if staging produces the same SHA on every machine. It does not by
# default: git reads configuration from the developer's ~/.gitconfig, from
# /etc/gitconfig, from an attributes file those name, and from environment
# variables, and several of those inputs change either the bytes that reach the
# index or the metadata in the commit object.
#
# When one of them fires, the gate fails with "fixture drift" on a checkout
# that has none. The fixture is byte-identical; the staged SHA is not. That
# diagnosis has been wrong at least twice, and the second time it cost a
# wrongly-edited cassette pin before the cause was found.
#
# WHY NOT A LIST OF KEYS
#
# The first attempt at this enumerated the keys that were known to matter --
# core.autocrlf, core.excludesfile, core.hooksPath, i18n.commitEncoding,
# commit.gpgsign, tag.gpgSign, init.templateDir. That is a denylist, and it was
# incomplete on the day it was written: a global core.attributesFile assigning
# a `clean` filter to Dockerfile rewrites the file's bytes on `git add`, moving
# container-ci-lineage's HEAD off its pin (measured on git 2.55.0:
# fe05491e -> f82875a3). Adding those two keys to the list would fix that one
# case and leave the next git release free to add another.
#
# So this does not enumerate. It switches the whole global and system
# configuration layer OFF for the duration of a fixture command, which is what
# the repository's own test suites do, and leaves the fixture's local
# .git/config -- written by staging, identical everywhere -- as the only
# configuration in effect.
#
# WHAT EACH INPUT IS FOR
#
#   GIT_CONFIG_GLOBAL=/dev/null   skips ~/.gitconfig and
#                                 $XDG_CONFIG_HOME/git/config.
#   GIT_CONFIG_SYSTEM=/dev/null   skips /etc/gitconfig (or wherever this build
#                                 of git put it).
#   -u GIT_CONFIG_COUNT           drops the GIT_CONFIG_KEY_n/GIT_CONFIG_VALUE_n
#                                 family. This is a SEPARATE vector and the one
#                                 that outranks the files: git applies those
#                                 pairs after reading every config file, so the
#                                 two /dev/null settings above do not stop
#                                 them. git only consults the pairs when
#                                 GIT_CONFIG_COUNT is set, so unsetting that
#                                 one variable disables all of them.
#   -u GIT_CONFIG_PARAMETERS      the same idea in git's internal serialization
#                                 of `-c`, inherited by any child git process.
#   GIT_ATTR_NOSYSTEM=1           skips the system gitattributes file, which is
#                                 a compiled-in path and therefore not
#                                 reachable by any config setting.
#   -c core.attributesFile=/dev/null
#                                 with global config gone, core.attributesFile
#                                 falls back to its DEFAULT path,
#                                 $XDG_CONFIG_HOME/git/attributes -- a file, not
#                                 a config key, so disabling config does not
#                                 disable it. Pointing it at /dev/null does.
#
# Identity and dates are NOT set here. They are pinned inline at each commit
# and tag site, because those sites do not all share one date, and a caller
# reading this file should see the pinned value next to the commit it produces.
#
# WHICH OF THOSE LINES A FAILING TEST ACTUALLY COVERS
#
# Stated because "every line here is tested" would be false, and an untested
# line nobody has flagged is how the denylist above survived as long as it did.
# Deleting the named line and running
# scripts/test-verify-golden-corpus-gate.sh gives, measured on git 2.55.0:
#
#   -u GIT_CONFIG_COUNT     RED. The hostile case injects core.excludesfile
#                           through GIT_CONFIG_KEY_0, HEAD moves to 247638a7
#                           with Dockerfile missing from the tree.
#   GIT_CONFIG_GLOBAL       green ALONE, RED together with the line below.
#   -c core.attributesFile  green ALONE, RED together with the line above.
#
# The middle two are redundant against the one vector the test can plant, a
# global core.attributesFile: either line stops it, so neither is falsifiable
# by itself. They are both kept because they cover different things beyond that
# vector -- GIT_CONFIG_GLOBAL covers every OTHER global key including ones no
# git release has shipped yet, which is the entire argument against the
# denylist, and the `-c` covers the default $XDG_CONFIG_HOME/git/attributes
# path, which is a file rather than a config key and so survives config being
# switched off.
#
# GIT_CONFIG_SYSTEM and GIT_ATTR_NOSYSTEM have no test and cannot get one here:
# planting either input means writing to /etc, which a test must not do.
#
# WHAT THIS DOES NOT COVER
#
# The fixture's own committed .gitattributes, if it ever gains one, is fixture
# content and is meant to apply. That is the intended behavior, not a leak.

# golden_corpus_git runs one git command with the developer's global and system
# configuration switched off. Use it for every git invocation that reads or
# writes a fixture repository the gate builds; a plain `git` call in that path
# is the defect this file exists to prevent.
#
# Inline environment assignments still reach it and still work, so a commit
# site keeps its pinned identity:
#
#   GIT_AUTHOR_NAME="Golden Gate" ... golden_corpus_git -C "${repo}" commit ...
golden_corpus_git() {
	env -u GIT_CONFIG_COUNT -u GIT_CONFIG_PARAMETERS \
		GIT_CONFIG_GLOBAL=/dev/null \
		GIT_CONFIG_SYSTEM=/dev/null \
		GIT_ATTR_NOSYSTEM=1 \
		git -c core.attributesFile=/dev/null "$@"
}
