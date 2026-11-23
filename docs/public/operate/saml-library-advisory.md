# SAML Library Advisory (GO-2026-6909)

**Who is affected:** deployments that configure SAML providers (see the
[environment variable reference](../reference/environment-variables.md)). SAML
sign-in is off unless an operator configures it.

**Fixed in:** the Eshu build that links upstream `github.com/crewjam/saml v0.5.2`
(see `go/go.mod`). Builds between the first fix and that build carried a fork,
`github.com/eshu-hq/saml v0.5.1-eshu.1`, through a `replace` directive. Earlier
builds link `crewjam/saml v0.5.1`.

## What the advisory says

`crewjam/saml` through v0.5.1 had no upstream fix when the advisory landed;
upstream v0.5.2 fixes it. Two defects sit in assertion validation:

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

- Carried a fork, `github.com/eshu-hq/saml`, tagged `v0.5.1-eshu.1`, while
  upstream had no fix. It rejected the three incomplete shapes and bound
  `NameID` to the SAML assertion namespace. Once the vulnerability database
  listed upstream v0.5.2 as the fixed version, the re-check below failed the
  build, and Eshu moved to upstream v0.5.2 and removed the `replace`. The
  regression tests named for the advisory
  (`TestVerifySAMLResponseGO20266909`) pass against upstream v0.5.2, including
  the incomplete-assertion cases and the foreign-namespace `NameID` cases, for
  plaintext and encrypted assertions.
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
  upstream. It also fails when the database query output is empty, is not
  JSON, or does not carry the expected `govulncheck` query envelope (a `config`
  message from protocol `v1.x`). The CI job installs `govulncheck@latest`, so a
  reshaped envelope or a new top-level message type turns the step red instead
  of reading as "no advisory".

## What operators should do

- Upgrade to the fixed build. No Eshu configuration changes. Upstream v0.5.2
  validates assertions more strictly than v0.5.1 and the fork did: it rejects
  an assertion with no `AudienceRestriction` in `Conditions` and an assertion
  with no bearer `SubjectConfirmation`. An identity provider that omits one of
  these now gets a `401` and an `assertion_invalid` audit event after the
  upgrade. Check the provider's assertion before you roll
  the build out to a deployment that uses SAML.
- Watch the panic counter. A non-zero value after the upgrade means a provider
  sent a shape the library still mishandles.
- Treat an assertion that changes a user's principal, with no change at the
  identity provider, as a security event and check the provider's signing
  configuration.

The fork is gone. The re-check stays: it covers any module that `go.mod`
replaces in the future. Whether to move off `crewjam/saml` is tracked in
[#10221](https://github.com/eshu-hq/eshu/issues/10221).
