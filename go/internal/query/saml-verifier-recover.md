# SAML verifier panic recovery (#10212)

`CrewjamSAMLVerifier.VerifySAMLResponse` now wraps the crewjam call in a
deferred `recover()` and `go.mod` replaces `github.com/crewjam/saml` with the
`eshu-hq/saml` fork (GO-2026-6909). The ACS handler's control flow is unchanged:
any verifier error already takes the `assertion_invalid` audit path and a `401`.

No-Regression Evidence: the change adds one deferred closure per SAML login
response and no query, lock, queue, or graph write. SAML sign-in is opt-in
(`ESHU_SAML_PROVIDERS_JSON`), runs once per interactive login, and is dominated
by RSA signature verification and XML canonicalization inside the library, not
by a `defer`. The fork adds three nil checks per assertion and an
assertion-namespace constraint on `NameID` decoding; neither adds work
proportional to input size. `go test ./internal/query -run 'SAML|GO20266909'
-count=1` passes. `TestVerifySAMLResponseGO20266909` fails on `crewjam/saml`
v0.5.1 (three panics and two accepted foreign `NameID` rows, plaintext and
inside an `EncryptedAssertion`) and passes on the fork.

Observability Evidence: one new counter, `eshu_dp_query_saml_verifier_panics_total`
(`samlVerifierPanics`, an `Int64Counter` registered once by
`initSAMLVerifierInstruments` in `saml_verifier_recover.go`), with no labels.
A recovered panic also logs an error line carrying `provider_config_id`,
`panic`, and `stack`, and reaches the existing `assertion_invalid`
governance-audit event. `TestVerifySAMLResponseRecoversCrewjamPanic` asserts the
error, the log line, and the counter value; `TestSAMLHandlerACSAuditsVerifierPanicAs401`
asserts the `401`, the audit reason code, and that no session is created. The
signal is documented in `docs/public/reference/telemetry/saml-verifier-panics.md`.
