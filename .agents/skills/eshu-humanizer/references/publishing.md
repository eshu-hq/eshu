# Publishing examples

### PR body

1. **What was broken** — plain words, no fix yet. Include *how* it broke, not
   just that it did. "`sed` exited 0 having replaced nothing" beats "the fixture
   didn't land."
2. **What changed** — and why. Include rejected alternatives only when they
   explain a material tradeoff in the final implementation.
3. **Evidence** — commands, numbers, results, and limits. Use a table when it
   helps compare results; a short sentence is enough for a simple change.
4. **What a reviewer would otherwise have to ask** — a coupling they will hit, a
   gap you left on purpose, a decision that needs an owner.

Title says what changed, not the ticket number:
`stop stamping drift findings exact when the address came from a guess` beats
`fix #5572`.

If review changed the approach, rewrite the title and body. A body describing
your first attempt sends reviewers looking for code that is not there.

### Review reply

What changed, where, and what proves it. If the reviewer was right and you had
argued otherwise, say so.

If their finding is now moot for a reason outside your diff, explain the reason:

```
Good:  This resolved itself when #5593 and #5623 merged — those rows live in
       main now, not in this PR. Your finding was right when you filed it.
```

Never close a thread with just "fixed."

### Issue close

Someone reads this in a year with no context. What was broken, what shipped,
and — if the issue's own plan was wrong — what it missed.
