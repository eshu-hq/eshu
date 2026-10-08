# Owner report

The owner follows many sessions at once and reads a page faster than terminal
text. A status or result report to the owner is one HTML page in STE-style
wording. The chat message stays short and points to it.

## When to use the page

- Use the page for a status update, a result, a plan, a proposal, or a
  comparison.
- Answer in chat when the owner asks you to explain something in plain English,
  or asks a direct question. Write the page after, if the answer is long.
- Coordination messages between agents stay plain text.

## Steps

1. Copy [the skeleton](owner-report-skeleton.html) to your scratch directory as
   `show-me-<topic>.html`. Do not write into the repository.
2. Fill the sections in the order below. Delete a section that has no content.
3. Run the STE linter on the page text when it is available.
4. Open the file. Use `open <file>` on macOS, or open it in a browser. The page
   fetches Mermaid from jsdelivr when it opens online. Offline, the diagram
   source stays readable.
5. Write 2 to 4 lines in chat: the state, and the file name.

## Page order

1. **Status.** One banner: `Done`, `Blocked`, `Needs your decision`, or
   `Working`. Add one sentence of why. Use `Needs your decision` only when the
   arbiter route cannot settle the question, or when only the owner can
   authorize the act. Otherwise report `Done` or `Working` and record the
   arbiter verdict.
2. **Answer.** The short answer in 2 to 4 sentences.
3. **Decision.** If the owner must decide, state it plainly with your
   recommendation and the cost of each option. Otherwise write "No decision
   needed."
4. **What changed or what we found.** A list or a Mermaid diagram. Use a file
   tree in `ls` form for a proposed change to files.
5. **Evidence.** A table: claim, evidence, who checked it (you or an agent), and
   a `Proven`, `Inferred`, or `NOT_CHECKED` tag.
6. **NOT_CHECKED.** Everything you did not check.
7. **Next step.** What happens now, who does it, and which sessions you told.

## Rules

- The owner's words are the test: "plainly state it", "what you need from me",
  "what is the recommendation". Answer those three first.
- A status request asks what you finished and what remains. Name each lane or
  session: its state, why it holds, and its next step. Do not write "nothing is
  blocked" without saying what each lane waits for.
- A claim about an artifact must match the artifact. If you say "the issue says
  X", X must be in the issue text. Quote it.
- Say the owner asked for something only when you can quote the request. Do not
  say "as you asked" from memory.
- Use the numbers and names the owner sees on GitHub, such as the PR and issue
  numbers, so the owner can open the item.
- Show the command and the result behind a claim, not only the conclusion. Put
  long output in a `<details>` block.
- Do not add gimmicks, such as "explained like you are 10".
- Keep pod IDs, image digests, entity IDs, and credentials out of the page.
- When you ask peer sessions to report to the owner, tell them to follow this
  file. Do not copy its rules into the message.
