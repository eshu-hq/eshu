# Codex Goal Hook

The `UserPromptSubmit` goal router in `.codex/hooks.json` was probed live
with `codex-cli 0.156.1` on 2026-09-24. The hook received JSON on stdin
with `cwd`, `prompt`, `hook_event_name: "UserPromptSubmit"`, `session_id`,
`turn_id`, `model`, `permission_mode`, and `transcript_path`. Its JSON
response used `hookSpecificOutput.hookEventName: "UserPromptSubmit"` and
`additionalContext`. The model answered with a marker present only in that
context, proving this event's wire shape and model-visible output on that
version. The probe used a vetted temporary hook passed through the session
config layer with `--dangerously-bypass-hook-trust`.

Project `.codex/hooks.json` loads only when the project config layer is
trusted. Codex also requires each new or changed hook definition to be
reviewed and trusted with `/hooks` in an interactive session. The bypass flag
above was for the isolated probe; normal Eshu sessions use `/hooks`. See the
[Codex hooks reference](https://developers.openai.com/codex/hooks) for the
event schema and trust behavior.

This probe pins `UserPromptSubmit` only. The Claude/Muse pre-tool and
session-start hook adapters require separate Codex payload probes before
porting.
