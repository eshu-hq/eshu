// LoginPage.tsx — production login surface.
// Supports local login (login_id + password + optional MFA recovery code).
// On mount, fetches GET /api/v0/auth/providers (public, no auth) — the
// tenant's derived AuthPosture (issue #5165, F-4) — and renders a "Continue
// with <label>" button per discovered OIDC/SAML provider (#3682).
// Local username/password login remains the DEFAULT/primary surface.
// If no providers are configured, no SSO buttons are rendered (current behavior).
// On successful local login, calls onSuccess(session) — caller navigates.
// Visuals match the approved auth mockup (authFlow.css) — elevated card,
// icon-led fields, password reveal, loading button state.
//
// Sign-in policy require_sso (#4968, epic #4962): the SAME posture fetch
// carries local_login_offered, derived from require_sso server-side (issue
// #5165 folded the console's former separate GET /api/v0/auth/sign-in-policy
// fetch into this one discovery call). When local_login_offered is false, the
// local password form is hidden UNLESS the URL carries ?local=1 — but that
// query param is a PURE UI HINT with no server-side meaning: POST
// /api/v0/auth/local/login applies the identical require_sso rule (session
// issued only for an admin identity) whether or not this hint was present,
// so hiding/showing the form here never changes what the server allows. This
// is why there is no client-side "am I an admin" check before rendering the
// form under ?local=1 — the server is the only authorization boundary; this
// page only decides what's convenient to show.
import {
  AlertTriangle,
  ChevronRight,
  Eye,
  EyeOff,
  Info,
  Lock,
  TriangleAlert,
  User,
} from "lucide-react";
import { useState, useEffect, type FormEvent } from "react";

import { AuthBrandMark } from "./AuthBrandMark";
import {
  loginLocal,
  listAuthPosture,
  beginOidcLogin,
  beginSamlLogin,
  beginGithubLogin,
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
  const [requireSSO, setRequireSSO] = useState(false);
  // #4964: the backend's default cookie policy keeps Secure=true (and thus
  // never persists the session) for any plain-HTTP origin outside loopback.
  // Detect that case up front so the diagnostic banner is visible before the
  // user ever submits credentials, not just after a confusing failed login.
  const showInsecureOriginBanner = isCookiePersistenceAtRiskOrigin(location);

  // ?local=1 is a console-only UI hint (#4968): it renders the local form
  // even when require_sso would otherwise hide it. It has NO server-side
  // meaning — POST /api/v0/auth/local/login enforces the identical
  // admin-only rule under require_sso whether or not this param is present.
  const localFormHint = new URLSearchParams(globalThis.location?.search ?? "").get("local") === "1";
  const showLocalForm = !requireSSO || localFormHint;

  // Fetch the tenant's derived sign-in posture on mount (issue #5165): one
  // call carries both the provider list and the require_sso-derived
  // local_login_offered flag, scoped by the tenant_id query param — without
  // it, the backend returns the safe zero-configuration default (empty
  // providers, local login offered). Errors are swallowed (listAuthPosture
  // itself fails open) so they never block the local login form.
  useEffect(() => {
    let cancelled = false;
    const tenantId =
      new URLSearchParams(globalThis.location?.search ?? "").get("tenant_id") ?? undefined;
    void listAuthPosture(client, tenantId).then((posture) => {
      if (cancelled) return;
      setProviders(posture.providers);
      setRequireSSO(!posture.local_login_offered);
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
          setErrorMsg("Enter your authenticator app code or a recovery code to continue.");
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
    if (provider.provider_kind === "oidc" || provider.provider_kind === "github") {
      // tenant_id must be forwarded from the page's own URL (the same param
      // the mount-time useEffect above reads for listAuthPosture): a
      // DB-backed provider's OIDC login-start
      // resolution (go/internal/oidclogin/service.go's provider()) requires
      // a non-empty tenantID to consult dbProviders at all, and returns
      // ErrOIDCLoginInvalidRequest (400) without it — verified live via a
      // real run's captured network response. An env/file-backed provider's
      // tenantID defaults from its own config either way
      // (resolveProviderContext), so this is a no-op for that case, but a
      // required fix for the DB-backed one this button is normally rendered
      // for. GitHub login-start (go/internal/githublogin/service.go's
      // provider(), issue #5166) has the identical requirement — same
      // reasoning applies unchanged.
      const tenantId =
        new URLSearchParams(globalThis.location?.search ?? "").get("tenant_id") ?? undefined;
      if (provider.provider_kind === "github") {
        beginGithubLogin(
          baseUrl,
          { providerConfigId: provider.provider_config_id, tenantId, returnTo },
          redirectFn ?? safeNavigate,
        );
      } else {
        beginOidcLogin(
          baseUrl,
          { providerConfigId: provider.provider_config_id, tenantId, returnTo },
          redirectFn ?? safeNavigate,
        );
      }
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

        {showLocalForm ? (
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
                <label htmlFor="login-mfa">Authenticator code or recovery code</label>
                <div className="input-shell">
                  <input
                    id="login-mfa"
                    type="text"
                    autoComplete="one-time-code"
                    placeholder="6-digit code or recovery code"
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
        ) : (
          <div className="note">
            <Info aria-hidden />
            <span>
              Your organization requires single sign-on. Continue with one of the providers below.
            </span>
          </div>
        )}

        {providers.length > 0 ? (
          <>
            {showLocalForm ? <div className="divider">or continue with</div> : null}
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
