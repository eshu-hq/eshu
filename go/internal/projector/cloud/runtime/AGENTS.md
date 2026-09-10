# AGENTS.md — cloud runtime projector namespace guidance

Read `README.md`, `doc.go`, `../AGENTS.md`, and `../../AGENTS.md` first.

Keep this namespace documentation-only. Runtime behavior belongs in leaves
under `drift/`, root assembly belongs in
`../../scope_generation_intents.go`, and materialization belongs in reducers.
Do not add shared trigger state or package initialization here.
