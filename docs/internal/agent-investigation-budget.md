# Investigation Budget

Use this procedure for open-ended diagnosis, performance measurement, remote
validation, access repair, or test infrastructure. It limits one approach, not
the authorized goal or a required safety gate.

## Start

1. Record the decision, cheapest existing test, pass/fail threshold, UTC start,
   deadline, and attempt count before work starts.
2. Use the task owner's stated limits. Otherwise, allow 30 minutes per theory and
   15 minutes for access or environment repair. Allow two theories per task
   and two attempts per theory.
3. Count preparation, scripts, retries, remote checks, and waits in the same
   wall-clock budget. A failed or inconclusive attempt MUST NOT reset it.

## Keep the test bounded

- Use an existing environment and the cheapest representative test first.
- Before building a clone, canary, reusable harness, IaC change, or data copy
  for an investigation, prove why the existing environment cannot safely answer.
- Estimate the time, resource use, and cost of that added setup. Get the task owner's
  explicit budget extension before starting it.
- Preserve the required correctness, privacy, performance, and review gates.
  A timebox MUST NOT authorize a weaker proof or a production shortcut.

## Stop and decide

1. At the deadline, stop the approach. Finish any safe teardown already needed.
2. Report the evidence, the result, and each remaining `NOT_CHECKED` item.
3. Choose one next state: implement a proved change, reject the theory, wait
   for a named external dependency, or request a budget extension.
4. Do not start a third theory or a third attempt on one theory without the
   task owner's budget extension. An arbiter can settle a technical fork but cannot
   reset the clock.

If an external dependency blocks the work, verify its owner and current state.
Use a watcher whose handle is confirmed live for later changes. Do not repeat
access checks or benchmarks until the dependency changes. Continue independent
in-scope work when possible.

If required proof remains unavailable, label the claim `NOT_PROVEN`. Keep the
goal open. Do not ship the claim or call the timebox a completed fix.
