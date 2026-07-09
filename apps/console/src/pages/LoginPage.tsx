// LoginPage.tsx — production login surface.
// Supports local login (login_id + password + optional MFA recovery code).
// On mount, fetches GET /api/v0/auth/providers (public, no auth) and renders a
// "Continue with <label>" button per discovered OIDC/SAML provider (#3682).
// Local username/password login remains the DEFAULT/primary surface.
// If no providers are configured, no SSO buttons are rendered (current behavior).
// On successful local login, calls onSuccess(session) — caller navigates.
import { useState, useEffect, type FormEvent } from "react";

import {
  loginLocal,
  listAuthProviders,
  beginOidcLogin,
  beginSamlLogin,
  isCookiePersistenceAtRiskOrigin,
  INSECURE_COOKIE_ORIGIN_MESSAGE,
} from "../api/authSession";
import type { AuthLoginProvider, InsecureCookieOrigin } from "../api/authSession";
import type { BrowserSessionResponse } from "../api/client";
import type { EshuApiClient } from "../api/client";

export interface LoginPageProps {
  readonly client: EshuApiClient;
  // onSuccess is called with the session after a successful local login.
  readonly onSuccess: (session: BrowserSessionResponse) => void;
  // baseUrl is required for OIDC/SAML redirect URL construction.
  readonly baseUrl?: string;
  // redirectFn is called with the constructed SSO redirect URL. Defaults to
  // location.assign. Injected in tests to avoid real navigation.
  readonly redirectFn?: (url: string) => void;
  // location is the origin used to detect an insecure, non-loopback plain-HTTP
  // context where the session cookie will not persist (#4964). Defaults to
  // globalThis.location. Injected in tests to avoid monkey-patching jsdom.
  readonly location?: InsecureCookieOrigin;
}

type LoginPhase = "credentials" | "mfa";

export function LoginPage({
  client,
  onSuccess,
  baseUrl = "",
  redirectFn,
  location = globalThis.location,
}: LoginPageProps): React.JSX.Element {
  const [login, setLogin] = useState("");
  const [password, setPassword] = useState("");
  const [mfaCode, setMfaCode] = useState("");
  const [phase, setPhase] = useState<LoginPhase>("credentials");
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [providers, setProviders] = useState<readonly AuthLoginProvider[]>([]);
  // #4964: the backend's default cookie policy keeps Secure=true (and thus
  // never persists the session) for any plain-HTTP origin outside loopback.
  // Detect that case up front so the diagnostic banner is visible before the
  // user ever submits credentials, not just after a confusing failed login.
  const showInsecureOriginBanner = isCookiePersistenceAtRiskOrigin(location);

  // Fetch available SSO providers on mount. The tenant_id query param scopes
  // the request to a single tenant — without it the endpoint returns empty.
  // Errors are swallowed so they never block the local login form.
  useEffect(() => {
    let cancelled = false;
    const tenantId =
      new URLSearchParams(globalThis.location?.search ?? "").get("tenant_id") ?? undefined;
    void listAuthProviders(client, tenantId).then((items) => {
      if (!cancelled) setProviders(items);
    });
    return () => {
      cancelled = true;
    };
  }, [client]);

  async function handleSubmit(e: FormEvent<HTMLFormElement>): Promise<void> {
    e.preventDefault();
    setSubmitting(true);
    setErrorMsg(null);
    try {
      const result = await loginLocal(client, {
        login: login.trim(),
        password,
        mfaCode: phase === "mfa" ? mfaCode.trim() : undefined,
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
              : "Account is temporarily locked. Try again later.",
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

  /**
   * Returns a safe same-origin return path from location.pathname, or "/" when
   * the path cannot be validated. Rejects open-redirect vectors: protocol-
   * relative URLs (//), absolute URLs (http:/https:), UNC paths (\\), and
   * values containing CRLF characters. Mirrors the Go safeOIDCReturnPath guard.
   */
  function safeSSOReturnPath(pathname: string | undefined): string {
    const path = (pathname ?? "").trim();
    if (!path.startsWith("/")) return "/";
    if (path.startsWith("//")) return "/";
    if (/^https?:/i.test(path)) return "/";
    if (path.startsWith("\\")) return "/";
    if (/[\r\n\t]/.test(path)) return "/";
    return path;
  }

  // safeNavigate parses the target through the URL API and navigates only to
  // http(s) destinations, rejecting javascript:/data: and other script-bearing
  // schemes before they reach location.assign.
  function safeNavigate(url: string): void {
    let target: URL;
    try {
      target = new URL(url, globalThis.location?.origin ?? undefined);
    } catch {
      return;
    }
    if (target.protocol === "http:" || target.protocol === "https:") {
      globalThis.location.assign(target.href);
    }
  }

  function handleSSOClick(provider: AuthLoginProvider): void {
    const returnTo = safeSSOReturnPath(globalThis.location?.pathname);
    if (provider.provider_kind === "oidc") {
      beginOidcLogin(
        baseUrl,
        { providerConfigId: provider.provider_config_id, returnTo },
        redirectFn ?? safeNavigate,
      );
    } else {
      beginSamlLogin(
        baseUrl,
        { providerId: provider.provider_config_id, returnTo },
        redirectFn ?? safeNavigate,
      );
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
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
        <h1 className="login-title">Sign in to Eshu</h1>

        {showInsecureOriginBanner ? (
          <div className="login-insecure-origin-warning" role="alert" aria-live="assertive">
            {INSECURE_COOKIE_ORIGIN_MESSAGE}
          </div>
        ) : null}

        {errorMsg !== null ? (
          <div className="login-error" role="alert" aria-live="assertive">
            {errorMsg}
          </div>
        ) : null}

        <form
          className="login-form"
          onSubmit={(e) => {
            void handleSubmit(e);
          }}
        >
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
          <button className="btn-primary login-submit" type="submit" disabled={submitting}>
            {submitting ? "Signing in…" : "Sign in"}
          </button>
        </form>

        {providers.length > 0 ? (
          <div className="login-sso">
            <div className="login-sso-divider" aria-hidden>
              or
            </div>
            {providers.map((provider) => (
              <button
                key={provider.provider_config_id}
                className="btn-secondary login-sso-btn"
                type="button"
                onClick={() => handleSSOClick(provider)}
              >
                Continue with {provider.display_label}
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}
