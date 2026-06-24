// LoginPage.tsx — production login surface.
// No API-key input. Supports local login (login_id + password + optional MFA
// recovery code). OIDC/SAML redirect buttons are hidden in Slice A pending
// provider-discovery endpoint (#3682). The helpers and tests remain in place.
// On successful local login, calls onSuccess(session) — caller navigates.
import { useState, type FormEvent } from "react";

import { loginLocal } from "../api/authSession";
import type { BrowserSessionResponse } from "../api/client";
import type { EshuApiClient } from "../api/client";

export interface LoginPageProps {
  readonly client: EshuApiClient;
  // onSuccess is called with the session after a successful local login.
  readonly onSuccess: (session: BrowserSessionResponse) => void;
  // baseUrl is kept for future OIDC/SAML redirect URLs (#3682).
  readonly baseUrl?: string;
}

type LoginPhase = "credentials" | "mfa";

export function LoginPage({ client, onSuccess }: LoginPageProps): React.JSX.Element {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [mfaCode, setMfaCode] = useState("");
  const [phase, setPhase] = useState<LoginPhase>("credentials");
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    setSubmitting(true);
    setErrorMsg(null);
    try {
      const result = await loginLocal(client, {
        login: login.trim(),
        password,
        mfaCode: phase === "mfa" ? mfaCode.trim() : undefined
      });
      switch (result.status) {
        case "ok":
          onSuccess(result.session);
          break;
        case "mfa_required":
          setPhase("mfa");
          setErrorMsg("Enter your recovery code to continue.");
          break;
        case "locked":
          setErrorMsg(
            result.lockedUntil
              ? `Account locked until ${new Date(result.lockedUntil).toLocaleString()}.`
              : "Account is temporarily locked. Try again later."
          );
          break;
        case "disabled":
          setErrorMsg("Account disabled — contact an admin.");
          break;
        case "invalid":
          setErrorMsg("Incorrect login or password.");
          break;
      }
    } catch (err) {
      setErrorMsg(err instanceof Error ? err.message : "An unexpected error occurred.");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <span className="brand-mark brand-glyph" aria-hidden><i /><i /><i /></span>
          <span><span className="brand-name">e<b>shu</b></span><span className="brand-sub">Context Graph</span></span>
        </div>
        <h1 className="login-title">Sign in to Eshu</h1>

        {errorMsg !== null ? (
          <div className="login-error" role="alert" aria-live="assertive">
            {errorMsg}
          </div>
        ) : null}

        <form className="login-form" onSubmit={(e) => { void handleSubmit(e); }}>
          <div className="login-field">
            <label htmlFor="login-id">Login</label>
            <input
              id="login-id"
              type="text"
              autoComplete="username"
              value={login}
              disabled={submitting}
              onChange={(e) => setLogin(e.target.value)}
              required
            />
          </div>
          <div className="login-field">
            <label htmlFor="login-password">Password</label>
            <input
              id="login-password"
              type="password"
              autoComplete="current-password"
              value={password}
              disabled={submitting}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </div>
          {phase === "mfa" ? (
            <div className="login-field">
              <label htmlFor="login-mfa">Recovery code</label>
              <input
                id="login-mfa"
                type="text"
                autoComplete="one-time-code"
                value={mfaCode}
                disabled={submitting}
                onChange={(e) => setMfaCode(e.target.value)}
              />
            </div>
          ) : null}
          <button
            className="btn-primary login-submit"
            type="submit"
            disabled={submitting}
          >
            {submitting ? "Signing in…" : "Sign in"}
          </button>
        </form>
        {/* OIDC and SAML sign-in buttons are hidden in Slice A pending
            provider-discovery endpoint implementation (#3682). */}
      </div>
    </div>
  );
}
