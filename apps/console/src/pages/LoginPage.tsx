// LoginPage.tsx — production login surface.
// No API-key input. Supports local login (login_id + password + optional MFA
// recovery code), OIDC provider redirect, and SAML provider redirect.
// On successful local login, calls onSuccess(session) — caller navigates.
import { useState, type FormEvent } from "react";
import type { BrowserSessionResponse } from "../api/client";
import { EshuApiHttpError } from "../api/client";
import type { EshuApiClient } from "../api/client";
import { loginLocal, beginOidcLogin } from "../api/authSession";

export interface LoginPageProps {
  readonly client: EshuApiClient;
  // onSuccess is called with the session after a successful login.
  readonly onSuccess: (session: BrowserSessionResponse) => void;
  // baseUrl is used for OIDC/SAML redirect URLs. Defaults to /eshu-api/.
  readonly baseUrl?: string;
}

type LoginPhase = "credentials" | "mfa";

// MFA_REQUIRED_CODE is the backend error code that signals MFA is needed.
const MFA_REQUIRED_CODE = "mfa_required";

export function LoginPage({ client, onSuccess, baseUrl = "/eshu-api/" }: LoginPageProps): React.JSX.Element {
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
      const session = await loginLocal(client, {
        login: login.trim(),
        password,
        mfaCode: phase === "mfa" ? mfaCode.trim() : undefined
      });
      onSuccess(session);
    } catch (err) {
      if (err instanceof EshuApiHttpError) {
        const code = err.error?.code ?? "";
        if (code === MFA_REQUIRED_CODE) {
          setPhase("mfa");
          setErrorMsg("Enter your recovery code to continue.");
        } else if (err.status === 401) {
          setErrorMsg("Login failed. Check your login ID and password.");
        } else {
          setErrorMsg(err.message);
        }
      } else if (err instanceof Error) {
        setErrorMsg(err.message);
      } else {
        setErrorMsg("An unexpected error occurred.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  function handleOidcSignIn(): void {
    beginOidcLogin(
      baseUrl,
      { providerConfigId: "", returnTo: "/" },
      (url) => { globalThis.location.assign(url); }
    );
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

        <div className="login-divider"><span>or</span></div>

        <div className="login-sso">
          <button
            className="btn-ghost login-oidc"
            type="button"
            onClick={handleOidcSignIn}
          >
            Continue with OIDC
          </button>
        </div>
      </div>
    </div>
  );
}
