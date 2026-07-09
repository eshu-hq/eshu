// SetupPage.tsx — first-run setup wizard (#4965).
// Three steps: (1) claim the generated one-time bootstrap credential,
// (2) replace it with an operator-chosen password, (3) enroll MFA recovery
// codes and complete setup. Every mutating call re-verifies the original
// claimed username/password against the still-unconsumed bootstrap
// credential (see go/internal/query/setup_handler.go's doc comment) — the
// wizard keeps them in state across steps instead of minting its own
// session, so there is nothing extra to steal or replay-protect client-side.
//
// Visuals match the approved auth mockup (authFlow.css): a persistent
// stepper header, a single Back/Continue/Finish footer, and — per the
// scoping decision below — recovery codes as the only *working* MFA
// control. TOTP enrollment is tracked separately in #4986: the login
// runtime (go/internal/storage/postgres/identity_local.go) only ever
// verifies MFARecoveryCodeHash today, so rendering a scannable QR here would
// be a dead control that looks functional but authenticates nothing. This
// page renders zero TOTP UI.
import { AlertTriangle, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState, type FormEvent } from "react";

import { AuthBrandMark } from "./AuthBrandMark";
import { SetupMFAStep } from "./SetupMFAStep";
import type { BrowserSessionResponse, EshuApiClient } from "../api/client";
import { claimSetup, completeSetupMFA, createSetupAdmin } from "../api/setupSession";
import "./authFlow.css";

export interface SetupPageProps {
  readonly client: EshuApiClient;
  // onSuccess is called with the session once the operator confirms they
  // saved their recovery codes — the same callback shape LoginPage uses, so
  // App.tsx's existing session-bootstrap flow needs no wizard-specific case.
  readonly onSuccess: (session: BrowserSessionResponse) => void;
}

type SetupStep = "claim" | "admin" | "mfa";

const STEPS: ReadonlyArray<{ id: SetupStep; k: string; t: string }> = [
  { id: "claim", k: "Step 1", t: "Claim" },
  { id: "admin", k: "Step 2", t: "Create admin" },
  { id: "mfa", k: "Step 3", t: "Secure" },
];

export function SetupPage({ client, onSuccess }: SetupPageProps): React.JSX.Element {
  const [step, setStep] = useState<SetupStep>("claim");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [codes, setCodes] = useState<readonly string[] | null>(null);
  const [session, setSession] = useState<BrowserSessionResponse | null>(null);
  const [saved, setSaved] = useState(false);
  const mfaRequested = useRef(false);
  const headingRef = useRef<HTMLHeadingElement>(null);

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

  // Entering step 3 immediately requests the recovery codes: arriving here
  // already required explicit operator intent (Continue from step 2), and
  // the codes must exist before the operator can be asked to confirm they
  // saved them. Guarded by mfaRequested so React StrictMode's double-invoke
  // (or a re-render) cannot double-submit this mutating, wizard-sealing call.
  useEffect(() => {
    if (step !== "mfa" || mfaRequested.current) return;
    mfaRequested.current = true;
    setSubmitting(true);
    setErrorMsg(null);
    void completeSetupMFA(client, { username: username.trim(), password }).then((result) => {
      setSubmitting(false);
      switch (result.status) {
        case "completed":
          setCodes(result.recoveryCodes);
          setSession(result.session);
          break;
        case "invalid":
          setErrorMsg(WRONG_CREDENTIAL_MESSAGE);
          break;
        case "gone":
          setErrorMsg(SETUP_UNAVAILABLE_MESSAGE);
          break;
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step]);

  function handleFinish(): void {
    if (!saved || !session) return;
    onSuccess(session);
  }

  const stepIndex = STEPS.findIndex((s) => s.id === step);
  const backDisabled = step === "claim" || step === "mfa";
  const finishDisabled = !saved || codes === null;

  return (
    <div className="login-page">
      <div className="auth-brand">
        <AuthBrandMark size={38} />
        <span className="auth-brand-name">
          e<b>shu</b>
        </span>
      </div>

      <section className="login-card setup-card" aria-label="First-run setup">
        <ol className="stepper" aria-label="Setup progress">
          {STEPS.map((s, i) => (
            <li
              key={s.id}
              style={{ display: "contents" }}
              aria-current={i === stepIndex ? "step" : undefined}
            >
              {i > 0 ? (
                <span className={`step-line${i <= stepIndex ? " fill" : ""}`} aria-hidden />
              ) : null}
              <div
                className="step"
                data-state={i < stepIndex ? "done" : i === stepIndex ? "active" : "upcoming"}
              >
                <span className="step-dot" aria-hidden>
                  {i < stepIndex ? <ChevronRight size={15} /> : i + 1}
                </span>
                <span className="step-txt">
                  <span className="step-k">{s.k}</span>
                  <span className="step-t">{s.t}</span>
                </span>
              </div>
            </li>
          ))}
        </ol>

        <div className="card-body">
          {errorMsg !== null ? (
            <div className="alert alert-err" role="alert" aria-live="assertive">
              <AlertTriangle aria-hidden />
              <span>{errorMsg}</span>
            </div>
          ) : null}

          {step === "claim" ? (
            <div className="stage">
              <div className="card-head">
                <h2 ref={headingRef} tabIndex={-1}>
                  Claim this instance
                </h2>
                <p>
                  This Eshu instance hasn&apos;t been set up yet. Enter the one-time admin
                  credential to prove you own the deployment.
                </p>
              </div>
              <form
                id="claim-form"
                onSubmit={(e) => {
                  void handleClaimSubmit(e);
                }}
              >
                <div className="field">
                  <label htmlFor="setup-username">Username</label>
                  <div className="input-shell">
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
                </div>
                <div className="field">
                  <label htmlFor="setup-password">One-time password</label>
                  <div className="input-shell mono">
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
                </div>
              </form>
              <div className="note">
                <AlertTriangle aria-hidden />
                <span>
                  Retrieve this value on the host where Eshu is running:{" "}
                  <code>eshu admin initial-credential</code>. It&apos;s printed once at first boot
                  and rotates after a successful claim.
                </span>
              </div>
            </div>
          ) : null}

          {step === "admin" ? (
            <AdminStage
              submitting={submitting}
              username={username}
              newPassword={newPassword}
              confirmPassword={confirmPassword}
              onNewPasswordChange={setNewPassword}
              onConfirmPasswordChange={setConfirmPassword}
              onSubmit={handleAdminSubmit}
              headingRef={headingRef}
            />
          ) : null}

          {step === "mfa" ? (
            <SetupMFAStep
              codes={codes}
              saved={saved}
              onSavedChange={setSaved}
              headingRef={headingRef}
            />
          ) : null}
        </div>

        <div className="card-foot">
          <span className="step-count">
            Step {stepIndex + 1} of {STEPS.length}
          </span>
          <div className="foot-btns">
            <button
              type="button"
              className="btn-ghost"
              disabled={backDisabled || submitting}
              onClick={() => setStep("claim")}
            >
              Back
            </button>
            {step !== "mfa" ? (
              <button
                type="submit"
                form={step === "claim" ? "claim-form" : "admin-form"}
                className="btn-primary"
                disabled={submitting}
                data-loading={submitting ? "true" : undefined}
              >
                <span className="spin" aria-hidden />
                <span className="btn-label">Continue</span>
                <ChevronRight aria-hidden size={16} />
              </button>
            ) : (
              <button
                type="button"
                className="btn-primary"
                disabled={finishDisabled}
                onClick={handleFinish}
              >
                Finish setup
              </button>
            )}
          </div>
        </div>
      </section>
    </div>
  );
}

interface AdminStageProps {
  readonly submitting: boolean;
  readonly username: string;
  readonly newPassword: string;
  readonly confirmPassword: string;
  readonly onNewPasswordChange: (value: string) => void;
  readonly onConfirmPasswordChange: (value: string) => void;
  readonly onSubmit: (e: FormEvent<HTMLFormElement>) => void;
  readonly headingRef: React.RefObject<HTMLHeadingElement | null>;
}

// AdminStage renders wizard step 2's form, including the password-strength
// meter (a pure client-side heuristic — it never talks to the backend and
// never blocks submission, matching the mockup exactly).
function AdminStage({
  submitting,
  username,
  newPassword,
  confirmPassword,
  onNewPasswordChange,
  onConfirmPasswordChange,
  onSubmit,
  headingRef,
}: AdminStageProps): React.JSX.Element {
  const strength = scorePassword(newPassword);
  return (
    <div className="stage">
      <div className="card-head">
        <h2 ref={headingRef} tabIndex={-1}>
          Create the admin account
        </h2>
        <p>
          This becomes the first owner of the workspace, with full control over members, providers,
          and data sources.
        </p>
      </div>
      <form id="admin-form" onSubmit={onSubmit}>
        <div className="field">
          <label htmlFor="setup-admin-username">Username</label>
          <div className="input-shell">
            <input id="setup-admin-username" type="text" value={username} readOnly />
          </div>
        </div>
        <div className="field">
          <label htmlFor="setup-new-password">New password</label>
          <div className="input-shell">
            <input
              id="setup-new-password"
              type="password"
              autoComplete="new-password"
              value={newPassword}
              disabled={submitting}
              onChange={(e) => onNewPasswordChange(e.target.value)}
              required
            />
          </div>
          <div className="strength">
            <div className="strength-bars">
              {[0, 1, 2, 3].map((i) => (
                <i
                  key={i}
                  style={{ background: i < strength.score ? strength.color : undefined }}
                />
              ))}
            </div>
            <div className="strength-row">
              <span>Use 12+ characters with a mix of cases, numbers &amp; symbols.</span>
              <b style={{ color: strength.score ? strength.color : undefined }}>{strength.label}</b>
            </div>
          </div>
        </div>
        <div className="field">
          <label htmlFor="setup-confirm-password">Confirm password</label>
          <div className="input-shell">
            <input
              id="setup-confirm-password"
              type="password"
              autoComplete="new-password"
              value={confirmPassword}
              disabled={submitting}
              onChange={(e) => onConfirmPasswordChange(e.target.value)}
              required
            />
          </div>
        </div>
      </form>
      <div className="note">
        <ChevronRight aria-hidden />
        <span>
          A default tenant and workspace are auto-created and assigned to this account. You can
          rename them later in Settings.
        </span>
      </div>
    </div>
  );
}

interface PasswordStrength {
  readonly score: number;
  readonly label: string;
  readonly color: string;
}

// scorePassword is a pure, client-side-only heuristic (never sent to the
// backend, never blocks submission) matching the approved mockup's scoring
// exactly: length and character-class diversity.
function scorePassword(value: string): PasswordStrength {
  let s = 0;
  if (value.length >= 8) s++;
  if (value.length >= 12) s++;
  if (/[a-z]/.test(value) && /[A-Z]/.test(value)) s++;
  if (/\d/.test(value)) s++;
  if (/[^A-Za-z0-9]/.test(value)) s++;
  const score = Math.min(4, Math.max(value ? 1 : 0, s - 1));
  const labels = ["", "Weak", "Fair", "Good", "Strong"];
  const colors = ["", "#f0506e", "#f5b73d", "#2dd4bf", "#2dd4bf"];
  return { score, label: labels[score] ?? "", color: colors[score] ?? "" };
}

const WRONG_CREDENTIAL_MESSAGE =
  "That credential is wrong or expired. Retrieve it again with `eshu admin initial-credential`, " +
  "or regenerate it with `eshu admin reset-initial-credential`.";

const SETUP_UNAVAILABLE_MESSAGE =
  "Setup is no longer available — this instance already has an administrator. Sign in instead.";
