# Wording

This skill uses Simplified Technical English (ASD-STE100) as a direction, not as
a certified standard. [ASD-STE100](https://www.asd-ste100.org/) is an
aerospace controlled language. Its authors built it so a reader who cannot ask a
question does not misread an instruction. An agent reading an issue, and the
owner skimming 10 sessions, are in the same position.

## What we can check, and what we cannot

| Kind | Rule | Checkable here |
|---|---|---|
| Structure | One instruction in each sentence. 20 words or fewer for a step, 25 for a description. | Yes |
| Structure | Active voice. Name the actor. | Yes |
| Structure | No semicolons. | Yes |
| Structure | No phrasal verbs ("set up", "take off"). | Yes |
| Structure | A noun cluster has 3 words or fewer. | By reading |
| Structure | One topic in each paragraph, 6 sentences or fewer. | By reading |
| Structure | Use a list for 3 or more steps. | By reading |
| Words | One word for one meaning, used the same way each time. | Within one text |
| Words | The verb, not the noun made from it ("check", not "perform a check"). | Yes |
| Words | Use only approved words in the approved sense. | No: the ASD dictionary is not public domain and is not in this repo |

Because the dictionary is absent, write "STE-style". Never write "ASD-STE100
compliant".

## Two modes

- **Strict.** Tool descriptions, error text, and instructions for an agent. Apply
  every rule above, including one word for one meaning. Also follow
  `docs/internal/writing-for-agents.md`, which adds `must` for a mandatory rule,
  `can` for permission, and no idioms. That page owns the shared sentence rules.
- **STE-flavored.** Issues, PR bodies, replies, and owner reports. Keep the
  structure rules. Relax the word-choice rules, because prose needs some range.

## Keep the claim

A short sentence is not worth a changed claim.

- Keep the strength of a hedge: "may have failed" stays "may have failed".
- Do not turn "could be caused by X" into "X is the cause".
- Do not add a cause, a frequency, or a mechanism that the source did not state.
- Keep a longer phrase when a shorter one drops a condition, a scope, or a
  number. Cutting words is not the goal. Removing doubt is the goal.
- STE fixes form, not substance. If the text has nothing to say, say that
  instead of polishing it.

## Exceptions in this repo

- Keep `MUST`, `MUST NOT`, and `NEVER` in capitals in hard rules.
- Keep necessary technical terms. Define a term once if a reader may not know
  it.
- Quoted evidence stays verbatim, even when it breaks these rules.

## Linter

The `ste-lint.py` script of the open-source asd-ste100 skill checks the
structure rules with regular expressions. It is a heuristic. It never flags
hedges. It is not part of this repo. Clone it to `~/os-repos` and run it with
`python3 -I`.

## Sources

- [danyuchn/asd-ste100-skill](https://github.com/danyuchn/asd-ste100-skill), MIT
  license. This file paraphrases its ideas: the structure and word split, the
  two modes, and keeping hedges.
- [ASD-STE100 official site](https://www.asd-ste100.org/)
- `docs/internal/writing-for-agents.md`, section "Wording standard".
