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

## Agent spawn probe

On 2026-09-28, a trusted Eshu worktree on Codex CLI 0.158.0 loaded the project
default as `gpt-6-luna` at high effort. In a separate run with a Sol low CLI
override on the parent, a generic `worker` child resolved to Luna high; its
token usage records were all on Luna high. A temporary `PreToolUse` hook at the
user level fired for `Bash`, while a hook
matching `Agent` did not fire for `spawn_agent`. A project hook with a catch-all
matcher likewise did not see the spawn. The same result held on CLI 0.157.0.

The current [Codex hook reference](https://developers.openai.com/codex/hooks)
lists `spawn_agent` under the `Agent` matcher but notes that specialized tool
paths can opt out of tool hooks. For this tested path, use the project model
defaults and named role files as the binding; do not claim a `PreToolUse`
spawn guard is active without a new live probe on the target Codex runtime.
