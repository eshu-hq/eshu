// LoginPage.tsx — production login surface.
// Supports local login (login_id + password + optional MFA recovery code).
// On mount, fetches GET /api/v0/auth/providers (public, no auth) and renders a
// "Continue with <label>" button per discovered OIDC/SAML provider (#3682).
// Local username/password login remains the DEFAULT/primary surface.
// If no providers are configured, no SSO buttons are rendered (current behavior).
// On successful local login, calls onSuccess(session) — caller navigates.
// Visuals match the approved auth mockup (authFlow.css) — elevated card,
// icon-led fields, password reveal, loading button state.
import { AlertTriangle, ChevronRight, Eye, EyeOff, Lock, TriangleAlert, User } from "lucide-react";
import { useState, useEffect, type FormEvent } from "react";

import { AuthBrandMark } from "./AuthBrandMark";
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
import "./authFlow.css";

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
  const [showPassword, setShowPassword] = useState(false);
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
      <div className="auth-brand">
        <AuthBrandMark />
        <span className="auth-brand-txt">
          <span className="auth-brand-name">
            e<b>shu</b>
          </span>
          <span className="auth-brand-sub">Context Graph</span>
        </span>
      </div>

      <section className="login-card" aria-labelledby="signin-title">
        <div className="card-head">
          <h1 id="signin-title">Sign in</h1>
          <p>Access your organization&apos;s code-to-cloud context graph.</p>
        </div>

        {showInsecureOriginBanner ? (
          <div className="alert alert-warn" role="alert" aria-live="assertive">
            <TriangleAlert aria-hidden />
            <span>{INSECURE_COOKIE_ORIGIN_MESSAGE}</span>
          </div>
        ) : null}

        {errorMsg !== null ? (
          <div className="alert alert-err" role="alert" aria-live="assertive">
            <AlertTriangle aria-hidden />
            <span>{errorMsg}</span>
          </div>
        ) : null}

        <form
          onSubmit={(e) => {
            void handleSubmit(e);
          }}
        >
          <div className="field">
            <label htmlFor="login-id">Login</label>
            <div className="input-shell lead-icon">
              <span className="input-lead" aria-hidden>
                <User />
              </span>
              <input
                id="login-id"
                type="text"
                autoComplete="username"
                placeholder="you@example.test"
                value={login}
                disabled={submitting}
                onChange={(e) => setLogin(e.target.value)}
                required
              />
            </div>
          </div>
          <div className="field">
            <label htmlFor="login-password">Password</label>
            <div className="input-shell lead-icon">
              <span className="input-lead" aria-hidden>
                <Lock />
              </span>
              <input
                id="login-password"
                type={showPassword ? "text" : "password"}
                autoComplete="current-password"
                value={password}
                disabled={submitting}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
              <button
                type="button"
                className="reveal"
                aria-label={showPassword ? "Hide password" : "Show password"}
                aria-pressed={showPassword}
                onClick={() => setShowPassword((v) => !v)}
              >
                {showPassword ? <EyeOff aria-hidden /> : <Eye aria-hidden />}
              </button>
            </div>
          </div>
          {phase === "mfa" ? (
            <div className="field">
              <label htmlFor="login-mfa">Recovery code</label>
              <div className="input-shell">
                <input
                  id="login-mfa"
                  type="text"
                  autoComplete="one-time-code"
                  value={mfaCode}
                  disabled={submitting}
                  onChange={(e) => setMfaCode(e.target.value)}
                />
              </div>
            </div>
          ) : null}
          <button
            className="btn-primary btn-block"
            type="submit"
            disabled={submitting}
            data-loading={submitting ? "true" : undefined}
          >
            <span className="spin" aria-hidden />
            <span className="btn-label">{submitting ? "Signing in…" : "Sign in"}</span>
          </button>
        </form>

        {providers.length > 0 ? (
          <>
            <div className="divider">or continue with</div>
            <div className="sso-stack">
              {providers.map((provider) => (
                <button
                  key={provider.provider_config_id}
                  className="btn-sso"
                  type="button"
                  onClick={() => handleSSOClick(provider)}
                >
                  Continue with {provider.display_label}
                  <ChevronRight aria-hidden className="chev" />
                </button>
              ))}
            </div>
          </>
        ) : null}
      </section>
    </div>
  );
}
