# Start here

Use this page to choose your first Eshu doc without reading the whole site.

If you are brand new, start with
[First Successful Run](getting-started/first-successful-run.md). It gets you
from zero to one indexed repository, a status check, and a first useful answer.

## Pick Your Goal

| Goal | First page |
| --- | --- |
| Get one complete local run | [First Successful Run](getting-started/first-successful-run.md) |
| Go from `docker compose up` to a logged-in dashboard with SSO | [Console First Five Minutes](getting-started/console-first-five-minutes.md) |
| Run Eshu on your laptop | [Run locally](run-locally/index.md) |
| Connect Codex, Claude, Cursor, VS Code, or another MCP client | [Connect MCP](mcp/index.md) |
| Index repositories and ask questions | [Use Eshu](use/index.md) |
| Trace a vulnerable dependency to its images and owners | [Use Cases](use-cases.md) |
| Audit secrets, IAM, and supply-chain posture | [Security Intelligence](reference/security-intelligence.md) |
| Deploy Eshu for a team | [Deploy to Kubernetes](deploy/kubernetes/index.md) |
| Deploy Eshu on EKS | [Deploy to EKS](deploy/eks/index.md) |
| Keep a running service healthy | [Operate Eshu](operate/index.md) |
| Debug slow indexing, stale answers, or graph timeouts | [Tuning Playbook](operate/tuning-playbook.md) |
| Understand the architecture and graph model | [Understand Eshu](understand/index.md) |
| Work on Eshu with an AI agent | [Code with agents](guides/coding-with-agents.md) |
| See what is planned before the next stable release | [Roadmap](roadmap.md) |
| Look up exact commands or flags | [CLI Reference](reference/cli-reference.md) |

## Two Local Paths

Most first-time readers start locally. Eshu has two local shapes:

- Use [Docker Compose](run-locally/docker-compose.md) when you want the full
  API and MCP service stack on one laptop.
- Use [Local binaries](run-locally/local-binaries.md) when you are developing
  Eshu or testing the local owner service behind `eshu graph start`.

If you are not sure which one you need, start with
[Run locally](run-locally/index.md).

## After Eshu Has Data

- [Index repositories](use/index-repositories.md)
- [Ask code questions](use/code-questions.md)
- [Trace infrastructure](use/trace-infrastructure.md)
- [Work through use cases](use-cases.md), from supply-chain triage to environment comparison
- [Use starter prompts](guides/starter-prompts.md)

## If You Are Operating Eshu

- [Health checks](operate/health-checks.md)
- [Telemetry](operate/telemetry.md)
- [Tuning playbook](operate/tuning-playbook.md)
- [Troubleshooting](operate/troubleshooting.md)
