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

## Active-goal Stop hook

`.codex/hooks.json` registers `.codex/hooks/goal-continue.sh` for Codex's
`Stop` event. It passes the event to the shared goal guard, which reads the
worktree's `.claude/active-goal.<session_id>` or shared
`.claude/active-goal`. An unfinished goal returns Codex's
`{"decision":"block","reason":"..."}` response, which starts a
continuation prompt. `DONE` and the verified live-watcher escape keep their
existing meaning, and the shared no-progress budget still bounds repeated
continuations that make no tool calls.

The official [Codex hooks reference](https://developers.openai.com/codex/hooks)
defines `Stop` inputs with `session_id`, `turn_id`, `stop_hook_active`, and
`last_assistant_message`, and documents the block response as a continuation.
An isolated probe with `codex-cli 0.164.0` exercised the committed hook files in
a temporary Git project: Codex's initial response stopped, the hook returned
the active goal, and Codex continued and marked the probe goal DONE. The probe
used `--dangerously-bypass-hook-trust`; it proves the event and response path,
not persisted trust in the interactive project.

When `last_assistant_message` contains capacity, security, blocked, denied,
error, or failed language, the adapter passes only a fixed diagnostic category
to the shared hook. Raw assistant text is never echoed into the continuation.
Capacity guidance asks Codex to retry or select another available model if the
current one remains unavailable; the hook does not switch models itself.

Codex supplies `turn_id` rather than `prompt_id`. The adapter keeps one budget
key across Stop continuations and resets it when a new owner turn starts. The
shared three-stop no-progress limit still applies. The DONE issue check is
bounded to one second total, and Codex blocks DONE when the ledger status cannot
be verified within that limit. The full shared hook is bounded to two seconds;
if a ledger lock or other check stalls, the adapter kills that process group and
returns a continuation block. The definition uses Codex's 3-second command-hook
timeout limit.

The hook contract has a hermetic regression suite at
`scripts/test-codex-goal-continue-hook.sh`. Project hooks only load after the
project config is trusted; review and trust the Stop hook with `/hooks` in an
interactive Codex session before relying on it.

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
its allowlist. The guard leaves explicit per-spawn model fields untouched. It
cannot determine whether the owner requested an override or which model the
spawn tool finally resolves; the coordinator must honor the goal's explicit
model choices and verify the child model. A separate 0.158.0 TUI probe ran the
committed guard through an isolated trusted hook source: a `default` spawn was
blocked by `PreToolUse`
with the allowed Eshu roles in the reason. The PR worktree's project hook
source still resolved to the sibling checkout, so this probe proved the guard
code and canonical matcher, not project-hook activation before merge. Because
non-managed hooks require trust, review the new definition with `/hooks` in an
interactive session. A future Codex version can change the canonical tool name;
repeat the live probe before claiming the guard works on that version.
