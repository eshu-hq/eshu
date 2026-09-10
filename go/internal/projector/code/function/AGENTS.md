# AGENTS.md — function projector namespace guidance

Read `README.md`, `doc.go`, `../AGENTS.md`, and `../../AGENTS.md` first.

Keep this namespace documentation-only. Runtime behavior belongs in leaf
packages, root assembly belongs in `../../scope_generation_intents.go`, and
materialization belongs in the reducer. Do not add cross-domain helpers here.
