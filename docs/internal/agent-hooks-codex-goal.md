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
default as `gpt-6-luna` at high effort. A generic child resolved to Luna high;
a named `debug-eshu` child resolved to Sol. Child token records confirmed the
models used. A temporary `PreToolUse` hook fired for a harmless Bash control.

The first spawn probe used the `Agent` matcher from the
[Codex hook reference](https://learn.chatgpt.com/docs/hooks), but it missed the
spawn tool. A catch-all probe revealed the canonical tool name
`collaborationspawn_agent`. A trusted hook matching that name rewrote a generic
spawn to `scan-eshu` through `permissionDecision: "allow"` and `updatedInput`;
the following `SubagentStart` reported `scan-eshu` on Luna. The temporary probe
was removed, and the worktree stayed clean. This 0.158.0 test proved the earlier
`Agent` matcher missed the spawn path; the 0.157.0 canonical name was not
retested.

The production guard matches the observed canonical name and requires a named
Eshu role. It refuses a generic or unknown role instead of assigning a scan
role to work whose intent it cannot infer. The guard uses the role manifest for
its allowlist. A selected role file supplies its model binding unless a
per-spawn model override is supplied. The guard cannot determine whether the
owner requested that override; the coordinator must honor the goal's explicit
model choices. A separate 0.158.0 TUI probe ran the committed guard through an
isolated trusted hook source: a `default` spawn was blocked by `PreToolUse`
with the allowed Eshu roles in the reason. The PR worktree's project hook
source still resolved to the sibling checkout, so this probe proved the guard
code and canonical matcher, not project-hook activation before merge. Because
non-managed hooks require trust, review the new definition with `/hooks` in an
interactive session. A future Codex version can change the canonical tool name;
repeat the live probe before claiming the guard works on that version.
