# Writing For Agents

This is Eshu's communication standard for conversations, progress updates,
technical explanations, commits, PRs, reviews, issues, and documentation. It is
part of the repository: contributors need no personal humanizer, global skill,
home-directory instruction file, or external writing linter.

The agent-document organization guidance adapts `writing-for-agents` from
[mattpocock/skills](https://github.com/mattpocock/skills) (MIT license,
Copyright (c) 2026 Matt Pocock) to Eshu, and adds a wording standard.

## Pointers decide when the agent reads

A **context pointer** is a line in the agent's context that names other material
and says when to read it. A skill description is one. A line in `AGENTS.md` that
names a doc is another. The wording of the pointer decides when the agent reads
the material. A must-have target behind a weak pointer causes variance: make the
pointer sharper before you copy the material inline.

- Put the leading word first.
- Write one trigger for each distinct branch. Two synonyms are one branch.
- Cut words the target already says.
- Every always-loaded word costs context on every turn. Prune pointers harder
  than body text.

## Where text goes

1. **Steps** go in the file, in order. Each step ends on a check that tells the
   agent the step is done.
2. **Reference** that every branch needs stays in the file, under one heading.
3. **Reference** that only some branches need goes in another file, behind a
   pointer.

Keep a definition, its rules, and its caveats together. Do not repeat a rule in
two files: keep one copy and point to it. A document that is too long is also a
defect, even if every line is true.

## Completion criteria

Write each step so that an agent can tell done from not done. "Every modified
model accounted for" is checkable. "Understanding reached" is not.

## Communication standard

Write as a capable colleague sitting beside the reader: friendly, clear, and
technically precise. Treat readers as intelligent adults with practical
experience who may be unfamiliar with this subject. Explain clearly to a capable
colleague who may be unfamiliar with the topic, rather than using a classroom
or fifth-grader framing.

- Use everyday language while preserving technical accuracy, reasoning,
  tradeoffs, and useful detail. Keep useful technical terms and explain unfamiliar
  ones briefly in context. Do not replace a technical explanation with an analogy.
- Be conversational, warm, and direct without forced friendliness, praise,
  slang, invented personal experience, or a classroom tone.
- Start with what matters. Match depth to the question and build on the existing
  conversation. A standalone issue, PR, or handoff still needs enough context for
  a teammate who did not follow the conversation.
- Give honest recommendations and disagree respectfully when warranted.
  Distinguish observations, assumptions, and uncertainty. Preserve evidence,
  caveats, exact identifiers, and the strength of claims.
- Avoid corporate language, academic padding, canned introductions, inflated
  claims, repetitive summaries, and unnecessary headings or bullets. Use structure
  when it helps readers act or compare options.
- Apply the style silently to original writing and rewrites. Return the finished
  answer or copy without drafts, an AI-pattern critique, or editing commentary
  unless requested. Required progress updates, engineering explanations, review
  evidence, and completion reports still apply.

Natural phrasing matters more than a word count. Sentence-length targets,
one-idea-per-sentence advice, and active voice are editing aids, not mechanical
bans. Keep necessary conditions together even when the sentence is longer. Use
consistent names for technical concepts without forcing every ordinary verb to
have only one synonym. Avoiding padding must not remove useful technical detail.

## Instructions and artifact formats

Write actionable steps with clear conditions and completion checks. Preserve
`MUST`, `MUST NOT`, and `NEVER` in hard rules; do not weaken an obligation to make
it sound friendlier. Keep quoted evidence, commands, and identifiers verbatim.

Commits state the concrete change and explain why when useful. Preserve required
commit conventions and trailers. PRs explain the problem, resulting behavior,
and relevant validation, with detail scaled to complexity. Review comments name
the behavior, its consequence, and a concrete next step when useful.

Use the repository's [publishing skill](../../.agents/skills/eshu-publish/SKILL.md)
for artifact-specific templates and checks. Preserve required issue-form fields,
PR formats, and engineering gates. A release checklist keeps its prerequisites
and monitoring sections instead of acquiring unrelated bug-report headings.
Follow an explicit request to preserve an existing format. This standard governs
prose; it does not relax accuracy, performance, concurrency, testing,
observability, security, or authorization requirements.

## License notice

The guidance above is adapted from `writing-for-agents` in
[mattpocock/skills](https://github.com/mattpocock/skills). Its license text
follows.

```text
MIT License

Copyright (c) 2026 Matt Pocock

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
