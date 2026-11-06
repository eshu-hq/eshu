# SAML Library Advisory (GO-2026-6909)

**Who is affected:** deployments that configure SAML providers (see the
[environment variable reference](../reference/environment-variables.md)). SAML
sign-in is off unless an operator configures it.

**Fixed in:** the Eshu build that pins `github.com/eshu-hq/saml v0.5.1-eshu.1`
(see `replace github.com/crewjam/saml` in `go/go.mod`). Earlier builds link
`crewjam/saml v0.5.1`.

## What the advisory says

`crewjam/saml` through v0.5.1 has no upstream fix. Two defects sit in assertion
validation:

- **Panics.** An assertion that omits `Subject`, `SubjectConfirmationData`, or
  `Conditions` made `ServiceProvider.ParseResponse` dereference a nil pointer.
  The API process survives, because `net/http` recovers a handler panic, but the
  request drops its connection and leaves no audit event.
- **Namespace-blind `NameID`.** The library matched a `NameID` element in any
  XML namespace, and the last match won. A `NameID` from a foreign namespace
  could set or override the principal Eshu derived from the assertion.

The advisory text also names `Assertion.Issuer`. That field is a value type in
v0.5.1, so a missing Issuer is rejected with an ordinary error and does not
panic. The advisory text does not match the library source on that point.

## Precondition

Every bad case needs an assertion that the identity provider signed. The
signature check runs before the assertion is decoded. An attacker therefore has
to be the identity provider, or has to shape content that the identity provider
signs. No unsigned request reaches these paths.

## What Eshu did

- Runs a fork, `github.com/eshu-hq/saml`, tagged `v0.5.1-eshu.1`. It rejects
  the three incomplete shapes and binds `NameID` to the SAML assertion
  namespace. The fork differs from upstream v0.5.1 only by those fixes and their
  tests.
- Recovers any panic that still gets through, answers `401`, writes an
  `assertion_invalid` audit event, logs the stack, and increments
  [`eshu_dp_query_saml_verifier_panics_total`](../reference/telemetry/saml-verifier-panics.md).
- Re-checks every module that `go.mod` replaces in the `govulncheck` CI job. The
  stock `govulncheck` scan looks advisories up by the replacement path, so a
  `replace` alone turns it green whether or not the fork fixes anything. The
  re-check asks the vulnerability database about the original module and
  version, then runs the regression tests named for each advisory twice: they
  must pass with the fork and fail on upstream. It fails when an advisory has no
  tests, when a fixed version appears upstream, or when the tests also pass on
  upstream.

## What operators should do

- Upgrade to the fixed build. No configuration change is needed.
- Watch the panic counter. A non-zero value after the upgrade means a provider
  sent a shape the fork still mishandles.
- Treat an assertion that changes a user's principal, with no change at the
  identity provider, as a security event and check the provider's signing
  configuration.

The fork is temporary. Retiring it (upstreaming the patch, reporting the
advisory mismatch to the Go vulnerability database, and deciding whether to move
off `crewjam/saml`) is tracked in a follow-up issue.
