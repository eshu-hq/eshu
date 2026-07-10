<!-- docs-catalog
title: Tutorials
description: Lists the hands-on tutorials that teach Eshu through complete reader outcomes.
type: tutorial
audience: new-user, practitioner, operator
entrypoint: false
landing: true
-->

# Tutorials

Use tutorials when you want to learn Eshu by completing a real outcome from
start to finish. Each tutorial names the goal, prerequisites, ordered steps,
expected result, failure hints, and what to read next.

## Start Here

<div class="grid cards">
<ul>
<li>
<p><strong>First successful run</strong></p>
<p>Start Eshu, index one repository, prove readiness, and ask one useful question.</p>
<p><a href="first-successful-run/">Start the first run</a></p>
</li>
<li>
<p><strong>Trace a vulnerable dependency</strong></p>
<p>Follow evidence from a vulnerable package to an affected workload and owning code.</p>
<p><a href="trace-vulnerable-dependency/">Trace the evidence</a></p>
</li>
<li>
<p><strong>Ask Eshu from an assistant</strong></p>
<p>Connect Codex, Claude, Cursor, VS Code, or another MCP client and ask a bounded question.</p>
<p><a href="ask-from-assistant/">Connect MCP</a></p>
</li>
<li>
<p><strong>Index repositories</strong></p>
<p>Point Eshu at repositories, wait for indexing completeness, and inspect the first results.</p>
<p><a href="index-repositories/">Index code</a></p>
</li>
<li>
<p><strong>Deploy on Kubernetes</strong></p>
<p>Render the Helm chart, install the core services, and verify the rollout.</p>
<p><a href="deploy-kubernetes/">Deploy with Helm</a></p>
</li>
<li>
<p><strong>Debug stale answers</strong></p>
<p>Separate process health from data freshness and find the runtime that is behind.</p>
<p><a href="debug-stale-answers/">Debug freshness</a></p>
</li>
</ul>
</div>

## Tutorial Map

| Tutorial | Use it when | Expected result |
| --- | --- | --- |
| [First successful run](first-successful-run.md) | You are new to Eshu and want one useful answer. | A completed run reports success only after a bounded query returns. |
| [Trace a vulnerable dependency](trace-vulnerable-dependency.md) | You want the supply-chain story from package evidence to workload impact. | The demo distinguishes offline scan proof from full-chain stack proof. |
| [Ask Eshu from an assistant](ask-from-assistant.md) | You want Codex, Claude, Cursor, VS Code, or another MCP client to use Eshu. | The client lists Eshu tools and answers a narrow first prompt. |
| [Index repositories](index-repositories.md) | You need Eshu to ingest code and infrastructure repositories. | `eshu list`, `eshu stats`, or index status shows indexed data. |
| [Deploy on Kubernetes](deploy-kubernetes.md) | You are preparing a shared Helm deployment. | Kubernetes workloads roll out and the API/MCP services are reachable. |
| [Debug stale answers](debug-stale-answers.md) | Health is green but answers are missing, stale, or incomplete. | Status endpoints identify whether ingestion, queueing, or graph writes are behind. |

## Read Next

- [How-to guides](../use/index.md) for task-focused instructions after a
  tutorial.
- [Concepts](../concepts/how-it-works.md) for the evidence graph model.
- [Reference](../reference/index.md) for exact commands and contracts.
