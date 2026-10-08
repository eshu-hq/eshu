# Owner report

The owner follows many sessions at once and reads a page faster than terminal
text. Each report back to the owner is one HTML page in STE-style wording,
opened with the `show-me` skill. The chat message stays short and points to it.

## When to use the page

- Use the page for a status update, a result, a plan, a proposal, or a
  comparison.
- Use plain chat for a one-line answer or a question to the owner.
- Coordination messages between agents stay plain text.

## Steps

1. Copy [the skeleton](owner-report-skeleton.html) to your scratch directory as
   `show-me-<topic>.html`. Do not write into the repository.
2. Fill the sections in the order below. Delete a section that has no content.
3. Run the STE linter on the page text when it is available.
4. Open it: `open <file>`.
5. Write 2 to 4 lines in chat: the state, and the file name.

## Page order

1. **Status.** One banner: `Done`, `Blocked`, `Needs your decision`, or
   `Working`. Add one sentence of why.
2. **Answer.** The short answer in 2 to 4 sentences.
3. **Decision.** If you wait on the owner, state it plainly with your
   recommendation and the cost of each option. If you do not wait, say
   "No decision needed."
4. **What changed or what we found.** A list or a Mermaid diagram. Use a file
   tree in `ls` form for a proposed change to files.
5. **Evidence.** A table: claim, evidence, who checked it (you or an agent), and
   a `Proven`, `Inferred`, or `Not checked` tag.
6. **Not checked.** Everything you did not check.
7. **Next step.** What happens now, and who does it.

## Rules

- The owner's words are the test: "plainly state it", "what you need from me",
  "what is the recommendation". Answer those three first.
- A claim about an artifact must match the artifact. If you say "the issue says
  X", X must be in the issue text. Quote it.
- Use the numbers and names the owner sees on GitHub, such as the PR and issue
  numbers, so the owner can open the item.
- Do not add gimmicks, such as "explained like you are 10".
- Keep pod IDs, image digests, entity IDs, and credentials out of the page.
- When you ask peer sessions to report to the owner, tell them to follow this
  file. Do not copy its rules into the message.
