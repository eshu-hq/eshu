// SetupPage.tsx — first-run setup wizard (#4965).
// Three steps: (1) claim the generated one-time bootstrap credential,
// (2) replace it with an operator-chosen password, (3) enroll MFA recovery
// codes and complete setup. Every mutating call re-verifies the original
// claimed username/password against the still-unconsumed bootstrap
// credential (see go/internal/query/setup_handler.go's doc comment) — the
// wizard keeps them in state across steps instead of minting its own
// session, so there is nothing extra to steal or replay-protect client-side.
// Reuses the same visual language as LoginPage.tsx (authFlow.css) so the
// operator's first screen looks like the same product as the rest of the
// console.
import { AlertTriangle, ShieldCheck } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";

import { SetupMFAStep } from "./SetupMFAStep";
import type { BrowserSessionResponse, EshuApiClient } from "../api/client";
import { claimSetup, createSetupAdmin } from "../api/setupSession";
import "./authFlow.css";

export interface SetupPageProps {
  readonly client: EshuApiClient;
  // onSuccess is called with the session once the operator confirms they
  // saved their recovery codes — the same callback shape LoginPage uses, so
  // App.tsx's existing session-bootstrap flow needs no wizard-specific case.
  readonly onSuccess: (session: BrowserSessionResponse) => void;
}

type SetupStep = "claim" | "admin" | "mfa";

const STEPS: ReadonlyArray<{ id: SetupStep; label: string }> = [
  { id: "claim", label: "Claim" },
  { id: "admin", label: "Administrator" },
  { id: "mfa", label: "Recovery codes" },
];

export function SetupPage({ client, onSuccess }: SetupPageProps): React.JSX.Element {
  const [step, setStep] = useState<SetupStep>("claim");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

  // Move focus to the new step's heading on every transition so screen-reader
  // and keyboard users get an explicit landmark instead of losing focus into
  // the removed step's DOM.
  useEffect(() => {
    headingRef.current?.focus();
  }, [step]);

  async function handleClaimSubmit(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    setSubmitting(true);
    setErrorMsg(null);
    const result = await claimSetup(client, { username: username.trim(), password });
    setSubmitting(false);
    switch (result.status) {
      case "claimed":
        setStep("admin");
        break;
      case "invalid":
        setErrorMsg(WRONG_CREDENTIAL_MESSAGE);
        break;
      case "gone":
        setErrorMsg(SETUP_UNAVAILABLE_MESSAGE);
        break;
    }
  }

  async function handleAdminSubmit(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    if (newPassword !== confirmPassword) {
      setErrorMsg("New password and confirmation do not match.");
      return;
    }
    setSubmitting(true);
    setErrorMsg(null);
    const result = await createSetupAdmin(client, {
      username: username.trim(),
      password,
      newPassword,
    });
    setSubmitting(false);
    switch (result.status) {
      case "admin_created":
        setStep("mfa");
        break;
      case "invalid":
        setErrorMsg(WRONG_CREDENTIAL_MESSAGE);
        setStep("claim");
        break;
      case "gone":
        setErrorMsg(SETUP_UNAVAILABLE_MESSAGE);
        break;
    }
  }

  const stepIndex = STEPS.findIndex((s) => s.id === step);

  return (
    <div className="login-page">
      <div className="login-card setup-card">
        <div className="login-brand">
          <span className="brand-mark brand-glyph" aria-hidden>
            <i />
            <i />
            <i />
          </span>
          <span>
            <span className="brand-name">
              e<b>shu</b>
            </span>
            <span className="brand-sub">Context Graph</span>
          </span>
        </div>

        <ol className="setup-stepper" aria-label="Setup progress">
          {STEPS.map((s, i) => (
            <li
              key={s.id}
              className="setup-step"
              data-state={i < stepIndex ? "done" : i === stepIndex ? "current" : "upcoming"}
              aria-current={i === stepIndex ? "step" : undefined}
            >
              <span className="setup-step-dot" aria-hidden>
                {i + 1}
              </span>
              {s.label}
            </li>
          ))}
        </ol>

        {errorMsg !== null ? (
          <div className="login-error" role="alert" aria-live="assertive">
            <AlertTriangle aria-hidden size={15} />
            <span>{errorMsg}</span>
          </div>
        ) : null}

        {step === "claim" ? (
          <>
            <h1 className="login-title" ref={headingRef} tabIndex={-1}>
              Claim this instance
            </h1>
            <p className="login-subtitle">
              Enter the one-time administrator credential printed to the server log at first start,
              or retrieved with <code className="mono">eshu admin initial-credential</code>.
            </p>
            <form
              className="login-form"
              onSubmit={(e) => {
                void handleClaimSubmit(e);
              }}
            >
              <div className="login-field">
                <label htmlFor="setup-username">Username</label>
                <input
                  id="setup-username"
                  type="text"
                  autoComplete="username"
                  value={username}
                  disabled={submitting}
                  onChange={(e) => setUsername(e.target.value)}
                  required
                />
              </div>
              <div className="login-field">
                <label htmlFor="setup-password">One-time password</label>
                <input
                  id="setup-password"
                  type="password"
                  autoComplete="current-password"
                  value={password}
                  disabled={submitting}
                  onChange={(e) => setPassword(e.target.value)}
                  required
                />
              </div>
              <button className="btn-primary login-submit" type="submit" disabled={submitting}>
                {submitting ? "Verifying…" : "Continue"}
              </button>
            </form>
          </>
        ) : null}

        {step === "admin" ? (
          <>
            <h1 className="login-title" ref={headingRef} tabIndex={-1}>
              Create the first administrator
            </h1>
            <p className="login-subtitle">
              Choose the password that replaces the one-time credential. This account owns the whole
              instance.
            </p>
            <form
              className="login-form"
              onSubmit={(e) => {
                void handleAdminSubmit(e);
              }}
            >
              <div className="login-field">
                <label htmlFor="setup-admin-username">Username</label>
                <input id="setup-admin-username" type="text" value={username} readOnly />
              </div>
              <div className="login-field">
                <label htmlFor="setup-new-password">New password</label>
                <input
                  id="setup-new-password"
                  type="password"
                  autoComplete="new-password"
                  value={newPassword}
                  disabled={submitting}
                  onChange={(e) => setNewPassword(e.target.value)}
                  required
                />
              </div>
              <div className="login-field">
                <label htmlFor="setup-confirm-password">Confirm password</label>
                <input
                  id="setup-confirm-password"
                  type="password"
                  autoComplete="new-password"
                  value={confirmPassword}
                  disabled={submitting}
                  onChange={(e) => setConfirmPassword(e.target.value)}
                  required
                />
              </div>
              <button className="btn-primary login-submit" type="submit" disabled={submitting}>
                {submitting ? "Saving…" : "Continue"}
              </button>
            </form>
          </>
        ) : null}

        {step === "mfa" ? (
          <SetupMFAStep
            client={client}
            username={username}
            password={password}
            headingRef={headingRef}
            onError={setErrorMsg}
            onSuccess={onSuccess}
          />
        ) : null}

        {step !== "mfa" ? (
          <p className="login-subtitle" style={{ marginTop: 18, marginBottom: 0 }}>
            <ShieldCheck aria-hidden size={13} style={{ verticalAlign: -2 }} /> This wizard is
            unreachable once setup is complete.
          </p>
        ) : null}
      </div>
    </div>
  );
}

const WRONG_CREDENTIAL_MESSAGE =
  "That credential is wrong or expired. Retrieve it again with `eshu admin initial-credential`, " +
  "or regenerate it with `eshu admin reset-initial-credential`.";

const SETUP_UNAVAILABLE_MESSAGE =
  "Setup is no longer available — this instance already has an administrator. Sign in instead.";
