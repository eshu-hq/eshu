# Writing For Agents

Use this page when you write or edit `AGENTS.md`, a skill, or any document an
agent reads. It adapts the `writing-for-agents` skill from
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

## Wording standard (Simplified Technical English)

Write steps and rules in the style of ASD-STE100:

- Write one instruction in each sentence. Use the imperative: "Run the test."
- Keep a procedure sentence to 20 words or fewer. Keep a descriptive sentence to
  25 words or fewer.
- Put the condition first: "If the test fails, fix the cause."
- Use one word for one meaning. Choose `run` or `execute`, not both.
- Use `must` only for a mandatory rule. Use `can` for permission.
- Use the active voice. Keep the articles (`the`, `a`).
- Do not use idioms or slang.

Eshu keeps the capital `MUST`, `MUST NOT`, and `NEVER` in hard rules, so that
an agent reads them as hard rules. This is a deliberate exception to STE.

A "leading word" (a pretrained concept such as "tracer bullet") is an idiom.
Use one only in a pointer or a definition, and define it once.

This standard comes from the public ASD-STE100 rules. Check a rewrite against the
public rules when a rule is in doubt.

This is STE-style wording, not certified STE. The sentence rules are checkable.
The approved-word list is not public domain, so the word rules are a direction.
For issues, PR bodies, replies, and owner reports, use the `eshu-publish` skill.
Its wording reference holds the split between checkable and uncheckable rules,
the strict and flavored modes, and the rule to keep every hedge.

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
