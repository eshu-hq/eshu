# Installing And Activating Agent Hooks

Split out of [Agent Hooks](agent-hooks.md) to keep that file under the repo's
500-line Markdown cap; this half covers activation and diagnosis rather than
hook behavior.

## Installing

The hook files and `.claude/settings.json` are committed, so once they are on
`main` any session rooted at the repo picks them up. Two rules decide whether
they actually load, and both were learned by watching them not load.

**Settings come from the session root, not the working directory.** Claude Code
reads project settings from the directory the session started in, and expands
`${CLAUDE_PROJECT_DIR}` to that same directory. A session rooted at the main
checkout that later moves into a worktree still uses the main checkout's
settings and hook paths. If the hooks exist only on a branch checked out in that
worktree, none of them register: the settings file that loaded never mentioned
them, and the paths it does mention resolve into a tree where they are absent.

Starting the session inside the worktree does not fix this, which cost two
attempts to learn: a session rooted at `.worktrees/<name>` still reported
`${CLAUDE_PROJECT_DIR}` as the main checkout. Project-scoped wiring therefore
cannot activate an unmerged hook from any worktree.

The scope that does work before merge is a **user-level install** in
`~/.claude/settings.json`, pointing at `~/.claude/hooks/eshu-hook.sh <name>`.
That dispatcher resolves the real script main-checkout-first and then the
worktree, and exits 0 quietly when it finds neither — so it keeps working after
the branch merges and the worktree is pruned, which a direct path into
`.worktrees/` does not: `bash` on a missing script exits 127 and prints on every
matching tool call, in every repository, starting the moment nobody is thinking
about hook wiring any more.

Note the consequence for iterating: a worktree session runs the **main
checkout's** copy of each hook, so a branch that edits a hook still executes
main's version. Script *content* is read at invocation, though, so editing a
hook the dispatcher already resolves takes effect immediately — only
`settings.json` changes need a reload.

**Settings are read at session start, and a resume counts.** A session already
running when the files arrive will not pick them up. Restarting the app and
resuming the same conversation does reload them, keeping the same
`CLAUDE_CODE_SESSION_ID` — that is how these hooks were first observed firing,
after four null results built on the assumption that only a brand-new
conversation could load them. Do not plan a hook test around that assumption;
make an edit on a governed surface and see whether it is refused. Approving the
hook permission prompt is part of activation, and declining it looks exactly
like a broken hook.

**The hook and the skill it names activate by different paths.** A user-level
install runs the hook from an absolute path, so it fires anywhere. The skill
*listing* still comes from the project directory, which for a worktree is the
main checkout. Before these skills reach `main`, a nudge can therefore name a
skill that `Skill(...)` reports as unknown. That is expected pre-merge; treat
the nudge as a pointer rather than a loadable reference until the branch lands.

Diagnosing a hook you think should have fired. When the nudge was advisory this
needed a stamp, because silence was ambiguous — it could mean the hook never
ran or that its output went somewhere you did not look. Blocking removes that
ambiguity for free: **a firing nudge fails your edit**, so you cannot miss it.

The question worth asking now is the opposite one. An edit on a governed
surface that is *not* refused has three explanations, in the order worth
checking:

1. The skill is already loaded. `ls /tmp/claude-skill-loaded-<session>-*` —
   `skill-loaded.sh` writes one marker per skill, and the nudge lifts when
   every id its arm names has one.
2. An override is in force: `/tmp/claude-skill-override-<session>` exists.
3. The hook is not wired. Check `.claude/settings.json` for the
   `(event, matcher, command)` triple, not just the filename —
   `scripts/test-agent-hooks.sh` asserts all four, and a hook attached to the
   wrong event is present, silent, and easy to mistake for absent.

Marker names take the first 12 characters of the session id, so a real one
looks like `claude-skill-loaded-aab79782-96c-golang-engineering`. Scope any
check to your own session id: markers are shared in `/tmp`, and reading another
session's is how a "confirmed" result turns out to be someone else's.

Nothing here bypasses `scripts/dev/bootstrap-hooks.sh`, which installs the git
pre-commit and pre-push hooks. The two sets are unrelated: git hooks gate
commits and pushes, agent hooks gate tool calls.

## Muse Code activation

The Muse port is project-level: `.muse/hooks.json` plus `.muse/hooks/`.
Settings are read at session start, so start a new session after changing
either. Project hooks load only in a trusted workspace -- an untrusted
session skips them silently, with no enforcement and no warning naming
hooks. `muse exec --trust-workspace` loads them for one run. Diagnose the
same three ways as above, reading `.muse/hooks.json` for the
(event, matcher, command) triple; `scripts/test-muse-hooks.sh` asserts all
three, and the Muse tool names differ (`write_file`, `edit_file`, `bash`,
`read_skill` -- a Claude `Write` matcher under Muse is present, silent,
and easy to mistake for absent). The envelope, output protocol, and limits
are in [Muse Hooks](agent-hooks-muse.md).
