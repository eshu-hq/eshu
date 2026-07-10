<!-- docs-catalog
title: Proof And Validation
description: Groups local gates, replay evidence, benchmarks, security checks, and runtime acceptance proof.
type: proof
audience: maintainer, operator
entrypoint: true
landing: false
-->

# Proof And Validation

Proof and validation pages preserve the evidence gates that make Eshu safe to
change and operate. They are maintainer- and operator-facing lookup material,
not first-run choices.

## What Lives Here

| Family | Examples |
| --- | --- |
| Local and CI gates | [Local Testing](local-testing.md), [Verification Gates](local-testing/verification-gates.md), and [Pre-commit Hooks](local-testing/pre-commit-hooks.md) |
| Replay and cassette proof | [Cassette And Replay Proof](cassette-replay.md) and [Replay Coverage Dashboard](replay-coverage.md) |
| Benchmark and scale evidence | [Search Benchmark Evidence](search-benchmark-evidence.md), [Scale Benchmark Artifact](local-testing/scale-benchmark-artifact.md), and [Parse Scale Evidence](parse-scale-evidence.md) |
| Collector and security proof | [All-Collector Readiness Proof Matrix](collector-readiness-proof-matrix.md), [Vulnerability Parity Gate](vulnerability-parity-gate.md), and [Security Intelligence Release Gate](security-intelligence-release-gate.md) |
| Runtime acceptance proof | [Remote E2E Runtime State](remote-e2e-runtime-state.md), [Remote Collector E2E](local-testing/remote-collector-e2e.md), and [Remote Representative Acceptance](local-testing/remote-representative-acceptance.md) |

## How To Use It

- New users should start with [First Successful Run](../getting-started/first-successful-run.md).
- Operators should start with [Operate](../operate/index.md) and come here for
  proof gates and acceptance evidence.
- Maintainers should cite the exact proof page and command in PRs instead of
  relying on a generic "docs build passed" claim.
