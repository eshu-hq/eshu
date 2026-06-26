# Dockerfile Parser Audit

## Overview
Extracts Dockerfile runtime evidence from source text using a `bufio` instruction scanner. This is a **build-manifest** parser — NOT a language parser. It parses Dockerfile instructions (FROM, COPY, ARG, ENV, EXPOSE, LABEL, ENTRYPOINT, CMD, HEALTHCHECK, USER, WORKDIR) and returns typed stage, port, arg, env, and label evidence. 3 src files, 1 test file. No regexp.MustCompile — pure line scanning.

## Claimed Constructs
From `doc.go`, `README.md`, `metadata.go`:
- **Stages**: name, line number, stage index, base image, base tag, platform, alias, path, copies-from, workdir, entrypoint, cmd, user, healthcheck
- **Args**: name, line number, default value, stage
- **Envs**: name, value, line number, stage
- **Ports**: name (stage:port), port, protocol, line number, stage
- **Labels**: name, value, line number, stage
- **Escape directive**: top-of-file `# escape=\` handling
- **Quoted metadata values**: ARG/ENV quoted key=value pairs
- **Legacy ENV key/value form**: `ENV KEY VALUE` (without =)
- **FROM platform and registry port**: `FROM --platform=$BUILDPLATFORM registry.example.com:5000/team/base:1.2`
- **Digest-based images**: `FROM alpine@sha256:abc123`
- **COPY --from**: extraction
- **Map() compatibility bridge**: preserves legacy payload bucket names for query callers

## Verified-by-Test Constructs
- `TestRuntimeMetadataExtractsStagesAndRuntimeSignals` (`metadata_test.go:11`): FROM stages, ARG default, ENV, EXPOSE with protocol, LABEL, COPY --from, ENTRYPOINT
- `TestRuntimeMetadataMapPreservesPayloadShape` (`metadata_test.go:47`): Map() output bucket names and typed row shapes for stages/ports/args/envs
- `TestRuntimeMetadataParsesFromPlatformAndRegistryPortTags` (`metadata_test.go:118`): FROM --platform, registry with port, digest images
- `TestRuntimeMetadataParsesMultipleArgsAndQuotedKeyValues` (`metadata_test.go:146`): multiple ARGs on one line, quoted values
- `TestRuntimeMetadataParsesQuotedMultilineEnvironment` (`metadata_test.go:`): ENV KEY=VALUE with quotes and multi-line
- `TestRuntimeMetadataParsesCurlFromAlpineImageTag` (`metadata_test.go:`): tag extraction from standard FROM lines
- `TestRuntimeMetadataParsesEntrypointCmdAndWorkdir` (`metadata_test.go:`): ENTRYPOINT, CMD, WORKDIR
- `TestRuntimeMetadataParsesHealthcheckAndUser` (`metadata_test.go:`): HEALTHCHECK, USER

## Unverified / Claimed-but-Untested Constructs
- **Escape directive**: claimed in doc.go but no dedicated test found
- **Quoted ENV key=value with embedded equals signs**
- **LABEL with multiple key=value pairs on one line**

## Edge Cases Considered
- FROM with --platform flag and registry port
- Digest-based images (no tag)
- Multiple stages with sorting (final stage first)
- COPY --from referencing external images (gcr.io style)
- Quoted ARG values
- Multi-line string values in ENV
- Single-line EXPOSE defaulting to tcp protocol
- ENTRYPOINT exec form (["/app"])

## Edge Cases NOT Considered
- Empty Dockerfile
- Syntax errors / unparseable lines
- ONBUILD instructions
- ADD instruction (not parsed)
- VOLUME instruction (not parsed)
- STOPSIGNAL instruction (not parsed)
- SHELL instruction (not parsed)
- Multi-line instruction continuation with `\` at end of line
- Comments and blank lines between instructions
- Mixed case instructions (e.g., `from` vs `FROM`)

## Verdict
**deep** — 1 test file (264 lines) with ~7 named tests covering stages, args, envs, ports, labels, platform, registry port, digest images, quoted values, ENTRYPOINT/CMD/WORKDIR/HEALTHCHECK/USER, and the Map() compatibility bridge. As a permanent exception build-manifest parser, this is well above the expected quality bar.

## Recommended Actions
- Document that DOCKERFILE is a **permanent exception** — it uses `bufio` instruction scanning, not tree-sitter
- Add a test for the escape directive (`# escape=\`)
- Add a test for ADD/ONBUILD (or document they are intentionally not parsed)
- Add a test for completely empty Dockerfile input
