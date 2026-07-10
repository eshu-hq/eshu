<!-- docs-catalog
title: Trace A Vulnerable Dependency
description: Walks through Eshu supply-chain evidence from package observation to workload impact.
type: tutorial
audience: security-engineer, practitioner
time: 15 minutes
entrypoint: true
landing: false
-->

# Tutorial: Trace A Vulnerable Dependency

Use this tutorial to see how Eshu follows supply-chain evidence from a package
observation to a workload impact story.

## Outcome

You will run the synthetic supply-chain demo path, understand which parts work
offline, and know when the full stack is required.

## Time

About 15 minutes for the offline scanner path. The full chain takes longer
because it needs a running stack and seeded synthetic facts.

## Prerequisites

- A local Eshu checkout.
- Go installed for building the CLI.
- Docker or OrbStack only if you run the full-chain proof.

## Steps

1. Build the CLI and helper service binaries from the checkout:

   ```bash
   ./scripts/install-local-binaries.sh
   export PATH="$(go env GOPATH)/bin:$PATH"
   ```

2. Scan the synthetic vulnerable app:

   ```bash
   eshu vuln-scan repo examples/supply-chain-demo/app --json
   ```

3. Run the missing-evidence variant:

   ```bash
   eshu vuln-scan repo examples/supply-chain-demo/missing-evidence --json
   echo "exit code: $?"
   ```

4. If you need the workload and image-identity hop, start the Compose stack and
   follow the full-chain proof script from the runbook:

   ```bash
   ESHU_SRC="$PWD" examples/supply-chain-demo/scripts/full-chain-proof.sh
   ```

5. Read the produced proof artifact and identify which sections came from an
   offline scan, seeded reducer proof, live stack proof, or fixture-only check.

## Expected Result

The offline app scan finds the synthetic package but does not promote a real
finding until Eshu owns advisory evidence. The missing-evidence path returns
`evidence_incomplete` and names `advisory_sources`. The full-chain proof shows
the evidence path through package, image, workload, and source ownership when
the stack and seeded facts are present.

## Failure Hints

- If a scan claims a synthetic CVE without owned advisory evidence, stop and
  re-check the runbook; the tutorial relies on honest evidence boundaries.
- If Docker is unavailable, run only the offline scanner and fixture checks.
- If image identity is empty, verify the image digest and SBOM subject match.
- If the proof output includes private hosts, paths, or credentials, discard it
  and rerun with synthetic or redacted values.

## Read Next

- [Supply-Chain Demo Runbook](../guides/supply-chain-demo.md) for the full
  command sequence and proof variants.
- [Supply-Chain Traceability](../supply-chain-traceability.md) for the product
  story.
- [SARIF Export](../reference/sarif-export.md) for export details.
