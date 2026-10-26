// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

/*
Command pr-watch is the one committed watcher for pull requests a session
owns. It polls every PR given on the command line and prints one line per
change, so a background Monitor wakes the agent only when something needs
action.

# Usage

	pr-watch [-repo owner/name] [-dir checkout] [-account login] [-interval 60s]
	         [-stable-reads 2] [-log-lines 12] [-max-polls N | -once]
	         [-required a,b] [-go go] [-gh-timeout 60s] [-git-timeout 120s] <pr>...

Run it in the background, once, over all open PRs the session owns, and
restart it with the full set whenever a new PR is created.

# Events

Every line is "<UTC timestamp> <KIND> [pr=N] key=value ...". Values with
spaces are quoted. An event prints once per change and never again for
unchanged state.

  - WATCH: watcher lifecycle. state=start, done, stopped, or exit, with a
    reason. The watcher never ends without one.
  - BASELINE: the first read of a PR: head, rollup, check count, merge state,
    and counts of unresolved threads, reviews, and comments already present.
  - CI-FAIL: a check run ended FAILURE, TIMED_OUT, CANCELLED, ACTION_REQUIRED,
    STARTUP_FAILURE, or STALE, or a status context ended ERROR. It carries the
    job name, result, run id, URL, and the last log lines (tail). A rerun that
    fails again is a new run and prints again.
  - CONFLICT: state=CONFLICTING, BEHIND, or UNKNOWN (UNKNOWN on two
    consecutive polls). It carries a local `git merge-tree` test merge of the
    exact base and head SHAs and a recipe label from the conflict-recipes list
    (.agents/skills/eshu-code-review/references/conflict-recipes.md):
    4a when every conflicted file is a recipe file (CHANGELOG.md, the collector
    content-version pin and its constant, the readiness weights, or a path in
    the GENERATED list of scripts/verify-live-postgres-readiness-drift.sh,
    which the watcher reads from the checkout); count-choice-lines otherwise,
    because the watcher cannot count choice lines (row 4a may still apply at
    10 or fewer choice lines in 2 or fewer files, else the conflict is row 4b);
    rebase when the test merge was clean; unknown when the test merge could
    not run. An agent-rule file (AGENTS.md, .agents/skills/**, .claude/rules/**,
    .claude/settings.json, .muse/hooks.json, .codex/**, docs/internal/agent-*.md,
    docs/internal/writing-for-agents.md) is never a recipe file and adds
    agent_rule_files=N. It prints once per (state, head, base).
  - NEW-THREAD, NEW-REVIEW, NEW-COMMENT: an unresolved review thread, a
    submitted review, or a conversation comment from any author, including
    this account. Deploy-preview and invite bots (cloudflare-workers-and-pages,
    github-actions) and mergify are ignored. A thread resolved and later
    reopened prints again.
  - HEAD-MOVED: the head SHA changed. source=local means the checkout's branch
    holds the new head (your push), foreign means it does not, unknown means
    the checkout is on another branch.
  - MERGED, CLOSED: the PR ended. The watcher stops polling it, and the agent
    stops pushing to its branch. When every PR has ended the watcher prints
    WATCH state=done and exits.
  - GREEN: CI is finished and stable. All of these hold on StableReads (default
    2) consecutive polls: the head's rollup state is SUCCESS and no check is
    pending; every check was read (the nodes match the rollup's own totalCount,
    which is why the checks are paged past the first 100); every merge-rule
    context is present and succeeded; and the head and the total check count
    did not change. The merge-rule contexts come from the gate registry
    (`ci-gates contexts --ruleset-only`, today go-core-complete and
    go-race-complete), read once at start (restart the watcher if the
    registry's ruleset contexts change), or from -required. If they cannot be
    read, or the list is empty, the watcher exits with a WATCH line. They are
    required because a fresh PR shows only its always-on external checks, all
    green, for minutes before the Actions wave registers; two stable reads of
    that set are not a finished CI. GREEN does not mean mergeable: it says
    nothing about review threads or mergeability (the line prints the merge
    state and the unresolved count), and a completed red anywhere, required or
    not, is already a CI-FAIL event.
  - ERROR: a read failed. It prints once per distinct message; the watcher
    keeps polling.

# Account pinning

Every gh call runs with GH_TOKEN set from `gh auth token --user <login>`,
read on every poll, after removing any ambient GH_TOKEN or GITHUB_TOKEN. The
login is -account, or linuxdynasty when -dir is under ~/personal-repos. Any
other path needs -account; the watcher exits with a WATCH line rather than
guess.

# Safety

Every gh and git child process has a deadline (-gh-timeout 60s, -git-timeout
120s). A call that exceeds it is killed, with its whole process group on unix
so no grandchild keeps running, and reported as an ERROR (or as the reason of
a CONFLICT test merge) naming the command; the poll goes on. Because each
child runs in its own process group, a run from an interactive terminal stops a
child that reads the terminal (an ssh passphrase prompt) with SIGTTIN until its
deadline kills it; the intended background use has no terminal. git children
run with GIT_TERMINAL_PROMPT=0.

The branch name and SHAs read from GitHub reach git only after validation: the
SHAs must be hex, the base branch must not start with "-" and must pass
`git check-ref-format`, and the refs follow "--" in the git call. A refused
test merge is reported as test_merge=unavailable with reason=refused.

# What it does not do

It is read-only toward GitHub. Its only local side effect is `git fetch` of the
base branch and refs/pull/N/head when a conflict needs a test merge, and that
fetch writes neither FETCH_HEAD nor a remote-tracking ref. It never pushes,
resolves a thread, replies, or merges. Threads, reviews, and comments read the
newest 100 of each per poll.
*/
package main
