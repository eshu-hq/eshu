# Build an evidence graph for your engineering stack

Start with one successful run, then connect code, dependencies,
infrastructure, runtime, and assistants into one queryable graph with evidence
at every hop.

[First successful run](getting-started/first-successful-run.md){ .md-button .md-button--primary }
[Connect MCP](mcp/index.md){ .md-button }

## Docs At A Glance

<div class="grid cards">
<ul>
<li>
<p><strong>Concepts</strong></p>
<p>Understand Eshu's data model, evidence graph, and core ideas.</p>
<p><a href="concepts/how-it-works/">Explore concepts</a></p>
</li>
<li>
<p><strong>How-to guides</strong></p>
<p>Complete focused tasks after Eshu is running.</p>
<p><a href="use/">Browse guides</a></p>
</li>
<li>
<p><strong>Tutorials</strong></p>
<p>Learn by doing with practical, end-to-end walkthroughs.</p>
<p><a href="tutorials/">View tutorials</a></p>
</li>
<li>
<p><strong>Reference</strong></p>
<p>Look up API, MCP, CLI, environment, and contract details.</p>
<p><a href="reference/">See reference</a></p>
</li>
</ul>
</div>

## Get Started

<div class="grid cards">
<ul>
<li>
<p><strong>New to Eshu</strong></p>
<p>Get from zero to one indexed repository, then choose the next path.</p>
<ul>
<li><a href="getting-started/first-successful-run/">First successful run</a></li>
<li><a href="start-here/">Start here</a></li>
<li><a href="use-cases/">What Eshu helps with</a></li>
</ul>
</li>
<li>
<p><strong>Using Eshu</strong></p>
<p>Index repositories, ask code questions, and connect an assistant through MCP.</p>
<ul>
<li><a href="use/index-repositories/">Index repositories</a></li>
<li><a href="use/code-questions/">Ask code questions</a></li>
<li><a href="mcp/">Connect MCP</a></li>
</ul>
</li>
<li>
<p><strong>Deploying Eshu</strong></p>
<p>Move from a local run to a shared Kubernetes service.</p>
<ul>
<li><a href="deploy/kubernetes/">Deployment options</a></li>
<li><a href="deploy/kubernetes/helm-quickstart/">Helm quickstart</a></li>
<li><a href="deploy/kubernetes/manifests/">Kubernetes manifests</a></li>
</ul>
</li>
<li>
<p><strong>Operating Eshu</strong></p>
<p>Check health, watch freshness, tune slow paths, and troubleshoot stale answers.</p>
<ul>
<li><a href="operate/health-checks/">Monitor health</a></li>
<li><a href="operate/freshness-convergence/">Manage data and indexes</a></li>
<li><a href="operate/troubleshooting/">Troubleshoot issues</a></li>
</ul>
</li>
</ul>
</div>

## Popular Tutorials

<div class="grid cards">
<ul>
<li>
<p><strong>Trace a vulnerable dependency</strong></p>
<p>Follow evidence from an alert to the images, workloads, and source that own it.</p>
<p><a href="tutorials/trace-vulnerable-dependency/">Open tutorial</a></p>
</li>
<li>
<p><strong>Ask Eshu from Codex</strong></p>
<p>Use natural language in an assistant to get evidence-backed answers from Eshu.</p>
<p><a href="tutorials/ask-from-assistant/">Connect MCP</a></p>
</li>
<li>
<p><strong>Index repositories</strong></p>
<p>Connect Git providers and index code at scale.</p>
<p><a href="tutorials/index-repositories/">Start indexing</a></p>
</li>
<li>
<p><strong>Deploy on Kubernetes</strong></p>
<p>Deploy Eshu with Helm and validate the rollout.</p>
<p><a href="tutorials/deploy-kubernetes/">Deploy with Helm</a></p>
</li>
<li>
<p><strong>Find unmanaged AWS resources</strong></p>
<p>Discover cloud inventory and trace resources back to owners.</p>
<p><a href="use/trace-infrastructure/">Trace infrastructure</a></p>
</li>
<li>
<p><strong>Debug stale answers</strong></p>
<p>Identify why an answer is outdated and fix the ingestion or reducer path.</p>
<p><a href="tutorials/debug-stale-answers/">Troubleshooting</a></p>
</li>
</ul>
</div>

## Explore By Role

| Role | Start here | Then read |
| --- | --- | --- |
| New engineer | [First successful run](getting-started/first-successful-run.md) | [How Eshu works](concepts/how-it-works.md) |
| Software engineer | [Ask code questions](use/code-questions.md) | [Starter prompts](guides/starter-prompts.md) |
| Platform engineer | [Trace infrastructure](use/trace-infrastructure.md) | [Deploy to Kubernetes](deploy/kubernetes/index.md) |
| Security engineer | [Supply-chain traceability](supply-chain-traceability.md) | [Security intelligence](reference/security-intelligence.md) |
| Operator | [Health checks](operate/health-checks.md) | [Tuning playbook](operate/tuning-playbook.md) |

## Quick Start

Run Eshu's first successful run, then connect an assistant through MCP:

```bash
eshu first-run
```

Read the [quick start guide](getting-started/first-successful-run.md), then
open [Connect MCP](mcp/index.md) when you are ready to ask questions from
Codex, Claude, Cursor, VS Code, or another MCP client.
