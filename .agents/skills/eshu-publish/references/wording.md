# Wording

Follow Eshu's [communication standard](../../../../docs/internal/writing-for-agents.md)
for original writing and rewriting. This reference adds examples and preserves
the distinction between clear prose and proof. It requires no global skill,
personal configuration, external word dictionary, or writing linter.

## Keep the claim

A shorter sentence is not worth a changed claim. Keep scope, conditions, numbers,
and the strength of uncertainty. "Could be caused by X" does not mean "X is the
cause". Keep necessary technical terms and explain unfamiliar ones briefly.
Never add a source, personal experience, mechanism, or test result merely to
make the text sound more natural.

Keep exact identifiers and quoted evidence. Explain surrounding shorthand in
ordinary language: "refresh the initial measurements" is clearer than "refresh
the T0 census". Use headings and lists for the decisions and steps the reader
needs, rather than to give every answer the same shape.

Review silently. Show only finished writing unless the user requests drafts,
a critique, or editing commentary. Required progress updates and engineering
reports still apply. Keep required templates, evidence, and approval boundaries.

## Technical reply

Example context: a charge may be accepted before its response times out. The
provider's support for idempotency keys has not been checked.

> More retries could charge the customer twice. The provider may have accepted
> the first charge before the response timed out, so another request could create
> a second charge. I recommend reusing one idempotency key for the same payment:
> a stable identifier that lets the provider recognize a retry. We still need to
> verify that the provider supports this behavior.

## Commit

For a hypothetical patch that reuses a payment key across retries:

```text
fix(payments): reuse the payment idempotency key across retries

Keep retries tied to the original payment so a timeout does not create a new
charge. Separate payments still receive separate keys.
```

Preserve the target repository's commit conventions and required trailers.

## PR description

For a hypothetical patch, suppose the timeout and distinct-payment tests passed,
but provider-sandbox validation has not run. Use the required Eshu PR template;
the following illustrates its problem, change, and proof wording only:

> A timeout can happen after the provider accepts a charge. Creating another key
> on retry can then charge the customer again. This change reuses the original
> payment key across retries and keeps separate payments distinct.
>
> The timeout-after-acceptance regression test and distinct-payments test pass.
> Provider-sandbox validation has not run, so this does not establish the
> provider's live behavior.

Report only tests actually run when writing a real PR. Do not copy fictional
verification into a publication.

## Review comment

For a patch that creates a new key inside the retry loop:

> This creates a new idempotency key on each retry. A provider that uses these
> keys to recognize repeated requests could treat each attempt as a new payment
> and charge twice after a timeout. Please create the key before the loop and add
> a regression test for a timeout after the provider accepts the first attempt.

Name the concrete behavior and consequence, then suggest a useful next step.
Do not pad the comment with praise or make the reader decode internal shorthand.

## Provenance

Earlier guidance drew on [danyuchn/asd-ste100-skill](https://github.com/danyuchn/asd-ste100-skill)
(MIT license) and public ASD-STE100 rules. Eshu now owns the communication
standard above; neither source is an installation or validation prerequisite.
