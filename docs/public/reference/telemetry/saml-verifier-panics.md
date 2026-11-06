# SAML Verifier Panic Signal

Eshu validates SAML sign-in responses with the `crewjam/saml` library. That
library panicked on signed but incomplete assertions
([GO-2026-6909](https://pkg.go.dev/vuln/GO-2026-6909); see
[SAML library advisory](../../operate/saml-library-advisory.md)). The
`eshu-hq/saml` fork fixes the known cases. The verifier also recovers any panic
that gets through, so a login fails closed instead of dropping the connection.

## Metric

| Metric | Type | Labels | Reading it |
| --- | --- | --- | --- |
| `eshu_dp_query_saml_verifier_panics_total` | counter | none | Panics recovered while verifying a SAML response. The recover covers the whole verify path (response decoding, metadata parsing, `crewjam/saml`, and Eshu's claim extraction), so the metric does not say which part failed; the logged `stack` does. The expected value is 0. A panic inside `crewjam/saml` usually means the identity provider sent an assertion the fork still dereferences badly: a defect to report, not an attack signature on its own. |

## What an operator sees when it fires

- The browser gets the same `401` every other failed SAML login gets.
- The governance audit records an `identity_authentication` event with reason
  code `assertion_invalid`.
- The API log carries an error line, `saml verifier: recovered panic while
  verifying a SAML response; login denied`, with `provider_config_id`,
  `panic`, and `stack` attributes. The stack names the line that panicked, in
  the library or in Eshu.
- The AuthnRequest is spent, because the ACS handler consumes it before it
  verifies the response. The user starts the login again.

## What to do

1. Alert on `increase(eshu_dp_query_saml_verifier_panics_total[15m]) > 0`.
2. Read the `stack` attribute on the matching log line and note the
   `provider_config_id`.
3. Ask the identity provider's administrator what that provider sent. The
   known triggers were an assertion with no `Subject`, no
   `SubjectConfirmationData`, or no `Conditions` element.
4. Report the stack to the `eshu-hq/saml` fork and to the tracking issue for
   retiring the fork.
