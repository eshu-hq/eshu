// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

const dockerfileWithArg = `ARG NODE_VERSION=20.11.1
FROM node:${NODE_VERSION} AS base
WORKDIR /app
FROM base AS final
`

func TestEmitDockerfileFromReadsResolvedTagsThroughArgSubstitution(t *testing.T) {
	t.Parallel()

	// The global ARG resolves before the FROM line is read, so the fact
	// carries the expanded tag, not the placeholder.
	got := emitDecoded(t, "Dockerfile", dockerfileWithArg)
	checkFacts(t, got, []wantFact{
		{".", "dockerfile_from", "node", "<nil>", "20.11.1", "exact", "<nil>", 2, 0, 20, 11, 1},
	})
}

func TestEmitDockerfileFromClassifiesTags(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, from, wantRaw, wantKind, wantRuntime, wantTool string
		major, minor, patch                                  int
	}{
		{"exact version", "FROM node:20.11.1", "20.11.1", SpecKindExact, "node", "<nil>", 20, 11, 1},
		{"partial major", "FROM node:20", "20", SpecKindPartial, "node", "<nil>", 20, -1, -1},
		{"partial with variant suffix", "FROM python:3.11-slim", "3.11-slim", SpecKindPartial, "python", "<nil>", 3, 11, -1},
		{"v prefix keeps raw", "FROM node:v20", "v20", SpecKindPartial, "node", "<nil>", 20, -1, -1},
		{"channel alias is never resolved", "FROM node:lts-iron", "lts-iron", SpecKindAlias, "node", "<nil>", -1, -1, -1},
		{"bare latest is an alias", "FROM node:latest", "latest", SpecKindAlias, "node", "<nil>", -1, -1, -1},
		{"distro name is an alias", "FROM debian:bookworm-slim", "bookworm-slim", SpecKindAlias, "debian", "<nil>", -1, -1, -1},
		{"golang image normalises to go", "FROM golang:1.22", "1.22", SpecKindPartial, "go", "golang", 1, 22, -1},
		{"registry path keeps the last segment", "FROM ghcr.io/acme/python:3.11", "3.11", SpecKindPartial, "python", "<nil>", 3, 11, -1},
		{"registry port is not a tag", "FROM localhost:5000/acme/app:1.2", "1.2", SpecKindPartial, "app", "<nil>", 1, 2, -1},
		{"range tag classifies as a range", "FROM acme/app:>=18", ">=18", SpecKindRange, "app", "<nil>", -1, -1, -1},
		{"unexpanded arg is unresolved", "FROM node:${NODE_VERSION}", "${NODE_VERSION}", SpecKindUnresolved, "node", "<nil>", -1, -1, -1},
		{"dollar name is unresolved", "FROM node:$NODE_VERSION", "$NODE_VERSION", SpecKindUnresolved, "node", "<nil>", -1, -1, -1},
		{"partially expanded tag stays unresolved", "FROM node:20-${VARIANT}", "20-${VARIANT}", SpecKindUnresolved, "node", "<nil>", -1, -1, -1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, "Dockerfile", tc.from+"\n")
			checkFacts(t, got, []wantFact{
				{".", "dockerfile_from", tc.wantRuntime, tc.wantTool, tc.wantRaw, tc.wantKind, "<nil>", 1, 0, tc.major, tc.minor, tc.patch},
			})
			if tc.wantKind != SpecKindExact && tc.wantKind != SpecKindPartial && tc.wantKind != SpecKindRange {
				for _, g := range got {
					if g.VersionMajor != nil || g.VersionMinor != nil || g.VersionPatch != nil {
						t.Errorf("%s %q carries version parts", tc.wantKind, tc.wantRaw)
					}
				}
			}
		})
	}
}

func TestEmitDockerfileFromSkipsLinesWithNoVersion(t *testing.T) {
	t.Parallel()

	// scratch, a digest pin and a bare image declare no tag version, and a
	// stage-local reference names no runtime, so none emits. Each is a
	// deliberate absence, never a guessed version.
	for name, body := range map[string]string{
		"scratch stage":       "FROM scratch\n",
		"digest pin":          "FROM node@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n",
		"bare image":          "FROM ubuntu\n",
		"unresolved image":    "FROM ${BASE_IMAGE}:20\n",
		"unresolved registry": "FROM ${REGISTRY}/app:1.2\n",
	} {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := emitDecoded(t, "Dockerfile", body); len(got) != 0 {
				t.Fatalf("emitted %+v, want 0", got)
			}
		})
	}
	// The local-stage-name body still emits its first stage.
	got := emitDecoded(t, "Dockerfile", "FROM node:20 AS base\nFROM base\n")
	checkFacts(t, got, []wantFact{
		{".", "dockerfile_from", "node", "<nil>", "20", "partial", "<nil>", 1, 0, 20, -1, -1},
	})
	// A reference whose image names no runtime is a counted drop, whether or
	// not it carries a tag: a variable is never silently swallowed.
	for _, body := range []string{"FROM ${BASE_IMAGE}:20\n", "FROM $IMAGE\n"} {
		envelopes, skip := EmitFile(testContext("Dockerfile"), "repo-1", "Dockerfile", body)
		if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
			t.Fatalf("EmitFile(%q) = %d envelopes, skip %+v, want 0 and one rejected value", body, len(envelopes), skip)
		}
	}
}

func TestEmitDockerfileFromStageLocalArgStaysUnresolved(t *testing.T) {
	t.Parallel()

	// An ARG declared after (or inside the stage of) its FROM line is not
	// visible to that FROM, so the tag stays unresolved even though a default
	// is written further down. Guessing the default would read a value the
	// build may override.
	body := "FROM node:${NODE_VERSION}\nARG NODE_VERSION=20\n"
	got := emitDecoded(t, "Dockerfile", body)
	checkFacts(t, got, []wantFact{
		{".", "dockerfile_from", "node", "<nil>", "${NODE_VERSION}", "unresolved", "<nil>", 1, 0, -1, -1, -1},
	})
}

func TestEmitDockerfileFromPredefinedBuildArgStaysUnresolved(t *testing.T) {
	t.Parallel()

	// Platform ARGs depend on the build host, so a tag that reads one is
	// unresolved on purpose rather than guessed, directly or through a global
	// ARG default. A --platform flag on the same line is parsed separately
	// and never leaks into the tag.
	got := emitDecoded(t, "Dockerfile", "FROM --platform=$BUILDPLATFORM golang:${TARGETARCH}\n")
	checkFacts(t, got, []wantFact{
		{".", "dockerfile_from", "go", "golang", "${TARGETARCH}", "unresolved", "<nil>", 1, 0, -1, -1, -1},
	})
	got = emitDecoded(t, "Dockerfile", "ARG TAG=1.22-${TARGETARCH}\nFROM golang:${TAG}\n")
	checkFacts(t, got, []wantFact{
		{".", "dockerfile_from", "go", "golang", "${TAG}", "unresolved", "<nil>", 2, 0, -1, -1, -1},
	})
}

func TestEmitDockerfileFromEmitsEveryStageInFileOrder(t *testing.T) {
	t.Parallel()

	body := "FROM node:20 AS base\nFROM python:3.11-slim\nFROM golang:1.22\n"
	got := emitDecoded(t, "services/api/Dockerfile", body)
	checkFacts(t, got, []wantFact{
		{"services/api", "dockerfile_from", "node", "<nil>", "20", "partial", "<nil>", 1, 0, 20, -1, -1},
		{"services/api", "dockerfile_from", "python", "<nil>", "3.11-slim", "partial", "<nil>", 2, 0, 3, 11, -1},
		{"services/api", "dockerfile_from", "go", "golang", "1.22", "partial", "<nil>", 3, 0, 1, 22, -1},
	})
}

func TestEmitDockerfileFromDropsUnresolvedFormsOutsideTheVersionGrammar(t *testing.T) {
	t.Parallel()

	// An unresolved tag outside the version grammar (a shell required-value
	// operator, a command substitution) is dropped and counted like any other
	// guard rejection, never guessed into a version. The same holds for the
	// same text in package.json: the guard is the contract.
	for name, body := range map[string]string{
		"required-value operator": "FROM node:${V:?missing}\n",
		"command substitution":    "FROM node:$(cat /etc/passwd)\n",
	} {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext("Dockerfile"), "repo-1", "Dockerfile", body)
			if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
				t.Fatalf("EmitFile = %d envelopes, skip %+v, want 0 and one rejected value", len(envelopes), skip)
			}
		})
	}
}

func TestEmitDockerfileFromRejectsCredentialShapedTags(t *testing.T) {
	t.Parallel()

	// Built by concatenation: GitHub push protection blocks the whole literal.
	token := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	body := "FROM node:" + token + "\nFROM node:20\n"
	envelopes, skip := EmitFile(testContext("Dockerfile"), "repo-1", "Dockerfile", body)
	if len(envelopes) != 1 {
		t.Fatalf("emitted %d envelopes, want only the safe neighbour", len(envelopes))
	}
	if skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
		t.Fatalf("skip = %+v, want one rejected value", skip)
	}
	for _, env := range envelopes {
		if strings.Contains(env.StableFactKey, token) {
			t.Fatalf("a fact key carries the credential-shaped tag")
		}
	}
}

func TestEmitDockerfileFromAdmitsDockerfileNames(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"Dockerfile", "services/api/Dockerfile", "Dockerfile.prod", "dockerfile"} {
		if got := emitDecoded(t, path, "FROM node:20\n"); len(got) != 1 {
			t.Errorf("%s emitted %d facts, want 1", path, len(got))
		}
	}
	// Suffixed variants read per the registry's Dockerfile. prefix rule, so a
	// Dockerfile.bak with valid FROM lines reads too; the README names that
	// edge. Names outside the rule never emit.
	for _, path := range []string{"notdockerfile", "Dockerfile2", "a/Dockerfilelock", "MyDockerfile", "docker-compose.yml"} {
		if got := emitDecoded(t, path, "FROM node:20\n"); len(got) != 0 {
			t.Errorf("%s emitted %d facts, want 0", path, len(got))
		}
	}
}

func TestEmitDockerfileFromSkipsOverTheManifestByteCap(t *testing.T) {
	t.Parallel()

	body := "FROM node:20\n" + strings.Repeat("# padding\n", MaxManifestBytesPerFile/10)
	envelopes, skip := EmitFile(testContext("Dockerfile"), "repo-1", "Dockerfile", body)
	if len(envelopes) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteCap != MaxManifestBytesPerFile {
		t.Fatalf("EmitFile = %d envelopes, skip %+v, want a byte-cap skip at the manifest cap", len(envelopes), skip)
	}
}
