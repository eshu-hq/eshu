// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

/*
Command pr-watch is the one committed watcher for pull requests a session
owns. It polls every PR given on the command line and prints one line per
change, so a background Monitor wakes the agent only when something needs
action.

# Usage

	pr-watch [-repo owner/name] [-dir checkout] [-account login] [-interval 60s]
	         [-stable-reads 2] [-log-lines 12] [-max-polls N | -once] <pr>...

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
  - GREEN: the head's rollup state is SUCCESS, no check is pending, and the
    head and total check count were identical on -stable-reads consecutive
    polls. It uses the rollup state plus its totalCount, not the first page of
    contexts, because contexts(first:100) hides a pending check past 100.
  - ERROR: a read failed. It prints once per distinct message; the watcher
    keeps polling.

# Account pinning

Every gh call runs with GH_TOKEN set from `gh auth token --user <login>`,
read on every poll, after removing any ambient GH_TOKEN or GITHUB_TOKEN. The
login is -account, or linuxdynasty when -dir is under ~/personal-repos. Any
other path needs -account; the watcher exits with a WATCH line rather than
guess.

# What it does not do

It is read-only toward GitHub. Its only local side effect is `git fetch` of the
base branch and refs/pull/N/head when a conflict needs a test merge. It never
pushes, resolves a thread, replies, or merges.
*/
package main
