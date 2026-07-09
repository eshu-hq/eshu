// SetupMFAStep.tsx — wizard step 3 content: recovery-code enrollment
// (#4965). Purely presentational — SetupPage.tsx owns the request that
// generates the codes (fired once on entering this step) and the Finish
// button in the persistent footer; this component only renders the result
// and the "I've saved these" confirmation gate.
//
// TOTP is intentionally NOT rendered here. #4986 tracks real TOTP
// enrollment + login verification; go/internal/storage/postgres/
// identity_local.go's AuthenticateLocalIdentity only ever checks
// MFARecoveryCodeHash today, so a scannable QR here would be a dead control
// that looks functional but authenticates nothing. The note below is
// text-only and non-interactive.
import { Check, Copy, Download, ShieldAlert } from "lucide-react";
import { useState } from "react";

export interface SetupMFAStepProps {
  readonly codes: readonly string[] | null;
  readonly saved: boolean;
  readonly onSavedChange: (saved: boolean) => void;
  readonly headingRef: React.RefObject<HTMLHeadingElement | null>;
}

export function SetupMFAStep({
  codes,
  saved,
  onSavedChange,
  headingRef,
}: SetupMFAStepProps): React.JSX.Element {
  const [copied, setCopied] = useState(false);

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

  if (codes === null) {
    return (
      <div className="stage">
        <div className="card-head">
          <h2 ref={headingRef} tabIndex={-1}>
            Securing your account
          </h2>
          <p>Generating your one-time recovery codes…</p>
        </div>
      </div>
    );
  }

  return (
    <div className="stage">
      <div className="card-head">
        <h2 ref={headingRef} tabIndex={-1}>
          Save your recovery codes
        </h2>
        <p>
          Each code signs you in once if you lose access to your password. They will not be shown
          again.
        </p>
      </div>

      <div className="note soon">
        <ShieldAlert aria-hidden />
        <span>
          Authenticator app (TOTP) — coming soon. Recovery codes are the supported MFA today.
        </span>
      </div>

      <p className="section-label">Store these somewhere safe</p>
      <div className="recovery">
        <div className="recovery-head">
          <ShieldAlert aria-hidden />
          <span>
            <strong>Each code works once.</strong>&nbsp;They&apos;re your only way back in if you
            lose your device.
          </span>
        </div>
        <ul className="codes" aria-label="Recovery codes">
          {codes.map((code) => (
            <li key={code}>{code}</li>
          ))}
        </ul>
        <div className="recovery-actions">
          <button
            className={`btn-mini${copied ? " ok" : ""}`}
            type="button"
            onClick={() => {
              void copyAll();
            }}
          >
            {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
            <span>{copied ? "Copied" : "Copy all"}</span>
          </button>
          <button className="btn-mini" type="button" onClick={download}>
            <Download aria-hidden />
            <span>Download .txt</span>
          </button>
        </div>
      </div>

      <label className="confirm" htmlFor="setup-codes-saved">
        <input
          id="setup-codes-saved"
          type="checkbox"
          checked={saved}
          onChange={(e) => onSavedChange(e.target.checked)}
        />
        <span className="checkbox" aria-hidden>
          <Check />
        </span>
        <span>
          I&apos;ve saved my recovery codes in a secure location and understand they won&apos;t be
          shown again.
        </span>
      </label>
    </div>
  );
}
