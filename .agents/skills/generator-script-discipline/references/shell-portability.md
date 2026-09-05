# Shell template portability

## Heredocs Deadlock On Large Bodies (bash >= 5.1) — Never Emit Data Through One

Issue #5019 (reopened after #5068 only patched the symptom): bash 5.1+
delivers a `<<EOF` heredoc body to its reader by writing the ENTIRE
body to a pipe before the reader process is even spawned. macOS's pipe
buffer is 512 bytes, so any heredoc body strictly between 512 bytes
and the ~64KB pipe-buffer ceiling deadlocks under Homebrew bash (5.1+)
while the same script runs fine under macOS's stock `/bin/bash`
(3.2.57, which never had this heredoc-writer change). A 10-13KB JSON
panel body — routine for a Grafana dashboard generator — sits
squarely in the hang zone. This is the same class of bug as the `<<<`
here-string hang fixed in #4718; treat both as "large body through a
shell here-construct" and route around it the same way.

**Rule: any generator whose body content exceeds a couple hundred
bytes MUST NOT use a `cat <<EOF` heredoc (or a `<<<` here-string) to
emit or capture it.** Use one of:

- A **template DATA FILE** (`scripts/lib/<name>-<part>.json.tmpl`)
  read with the `$(<file)` builtin and emitted with `printf '%s'`.
  Neither construct touches a pipe, so neither can hang. This is the
  pattern the operator dashboard generator now uses: `${NAME}` tokens
  in the template are substituted via an explicit allowlist loop
  (`scripts/lib/operator-dashboard-metrics.sh`'s
  `OPERATOR_DASHBOARD_METRIC_VARS`), and the literal Grafana
  `${DS_PROMETHEUS}` / `$__all` tokens pass through untouched because
  they are never looked up.
- If a function must still assemble the body in-process, emit it with
  `printf` — a builtin, so no pipe and no fork — either one `printf`
  per fragment or a single `printf '%s'` over a variable built by
  concatenation. Do NOT reach for `cat <<EOF` here: the bash 5.1+
  deadlock is in how the heredoc body is delivered to `cat` (the whole
  body is written to a pipe before `cat` is forked), so redirecting
  `cat`'s stdout to a real file (`cat <<EOF >file`) does not help — the
  body still traverses the pipe and hangs in the 512 B–64 KB range.
  That redirect-to-file shape is exactly what #5068 shipped for the
  operator dashboard, and it still hung. Capturing via
  `$(function_name)` is also out (large-input-in-command-substitution
  hangs the same way).

A generator that still uses a heredoc for a small (well under 512
bytes), fixed, non-data body — a one-line usage message, for example
— is fine; the hang only bites past the pipe-buffer threshold.
