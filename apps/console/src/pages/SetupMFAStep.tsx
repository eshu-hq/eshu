// SetupMFAStep.tsx — wizard step 3: enroll MFA recovery codes and complete
// first-run setup (#4965). Split out of SetupPage.tsx to keep both files
// small and focused, mirroring the backend's own setup_handler.go /
// setup_mfa_handler.go split.
//
// The codes are rendered exactly once and the wizard does not advance past
// this step until the operator explicitly confirms they saved them — see
// the "I've saved these" checkbox gating the Finish button below. Codes are
// never sent anywhere but this one response body; there is no re-fetch path.
import { Check, Copy, Download } from "lucide-react";
import { useState, type RefObject } from "react";

import type { BrowserSessionResponse, EshuApiClient } from "../api/client";
import { completeSetupMFA } from "../api/setupSession";

export interface SetupMFAStepProps {
  readonly client: EshuApiClient;
  readonly username: string;
  readonly password: string;
  readonly headingRef: RefObject<HTMLHeadingElement | null>;
  readonly onError: (message: string | null) => void;
  readonly onSuccess: (session: BrowserSessionResponse) => void;
}

export function SetupMFAStep({
  client,
  username,
  password,
  headingRef,
  onError,
  onSuccess,
}: SetupMFAStepProps): React.JSX.Element {
  const [submitting, setSubmitting] = useState(false);
  const [codes, setCodes] = useState<readonly string[] | null>(null);
  const [session, setSession] = useState<BrowserSessionResponse | null>(null);
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState(false);

  async function handleGenerate(): Promise<void> {
    setSubmitting(true);
    onError(null);
    const result = await completeSetupMFA(client, { username, password });
    setSubmitting(false);
    switch (result.status) {
      case "completed":
        setCodes(result.recoveryCodes);
        setSession(result.session);
        break;
      case "invalid":
        onError(
          "That credential is wrong or expired. Retrieve it again with " +
            "`eshu admin initial-credential`, or regenerate it with `eshu admin reset-initial-credential`.",
        );
        break;
      case "gone":
        onError("Setup is no longer available — this instance already has an administrator.");
        break;
    }
  }

  async function copyAll(): Promise<void> {
    if (!codes) return;
    try {
      await navigator.clipboard.writeText(codes.join("\n"));
      setCopied(true);
      setTimeout(() => setCopied(false), 1600);
    } catch {
      // Clipboard access can be denied; Download remains available.
    }
  }

  function download(): void {
    if (!codes) return;
    const blob = new Blob([codes.join("\n") + "\n"], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "eshu-recovery-codes.txt";
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  }

  function finish(): void {
    if (!saved || !session) return;
    onSuccess(session);
  }

  if (codes === null) {
    return (
      <>
        <h1 className="login-title" ref={headingRef} tabIndex={-1}>
          Enroll recovery codes
        </h1>
        <p className="login-subtitle">
          Eshu generates ten one-time recovery codes for this account. They are shown once — save
          them somewhere safe before continuing.
        </p>
        <button
          className="btn-primary login-submit"
          type="button"
          disabled={submitting}
          onClick={() => {
            void handleGenerate();
          }}
        >
          {submitting ? "Generating…" : "Generate recovery codes"}
        </button>
      </>
    );
  }

  return (
    <>
      <h1 className="login-title" ref={headingRef} tabIndex={-1}>
        Save your recovery codes
      </h1>
      <p className="login-subtitle">
        Each code signs you in once if you lose access to your password. They will not be shown
        again.
      </p>
      <ul className="setup-recovery-codes" aria-label="Recovery codes">
        {codes.map((code) => (
          <li key={code}>{code}</li>
        ))}
      </ul>
      <div className="setup-recovery-actions">
        <button
          className="btn-secondary"
          type="button"
          onClick={() => {
            void copyAll();
          }}
        >
          {copied ? (
            <>
              <Check aria-hidden size={14} /> Copied
            </>
          ) : (
            <>
              <Copy aria-hidden size={14} /> Copy all
            </>
          )}
        </button>
        <button className="btn-secondary" type="button" onClick={download}>
          <Download aria-hidden size={14} /> Download
        </button>
      </div>
      <label className="setup-confirm" htmlFor="setup-codes-saved">
        <input
          id="setup-codes-saved"
          type="checkbox"
          checked={saved}
          onChange={(e) => setSaved(e.target.checked)}
        />
        I've saved these recovery codes somewhere safe.
      </label>
      <button className="btn-primary login-submit" type="button" disabled={!saved} onClick={finish}>
        Finish setup
      </button>
    </>
  );
}
