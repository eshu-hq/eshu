// pages/TOTPEnrollmentControl.tsx — self-service authenticator-app (TOTP)
// enrollment control, extracted out of ProfilePage.tsx (issue #5072) to
// keep that file under the repo's 500-line cap.
//
// Flow: begin -> render a scannable QR of the otpauth:// URI plus the URI
// and manual key as an accessible text fallback (never re-fetchable after
// this render) -> submit the first code -> confirm. The QR (issue #5072)
// is built from a vendored, dependency-free byte-mode QR encoder
// (../lib/qrCode.ts) — see that file for attribution. Recovery codes remain
// the primary enrollment path from the setup wizard (SetupMFAStep.tsx);
// this is the ADDITIONAL self-service factor a signed-in user may enable
// for themselves at any time.
import { useState } from "react";

import type { EshuApiClient } from "../api/client";
import { beginTOTPEnrollment, confirmTOTPEnrollment } from "../api/totpEnrollment";
import type { TOTPBeginResult } from "../api/totpEnrollment";
import { encodeQrMatrix, qrMatrixToSvg } from "../lib/qrCode";

import "./totpEnrollment.css";

type TOTPEnrollmentPhase = "idle" | "provisioning" | "confirming" | "activated";

// TOTPEnrollmentQr renders a scannable QR code of the otpauth:// URI. The
// QR is decorative-with-text-alternative: the aria-label names it, and the
// caller keeps the Provisioning URI / Manual entry key inputs as the full
// non-visual fallback (screen reader, no camera). Only rendered when
// otpauthUri is a non-empty string; the secret itself is never logged —
// otpauthUri already carries it and only ever lives in memory from the
// begin response.
function TOTPEnrollmentQr({
  otpauthUri,
}: {
  readonly otpauthUri: string;
}): React.JSX.Element | null {
  if (otpauthUri.length === 0) return null;
  const matrix = encodeQrMatrix(otpauthUri);
  const { path, size } = qrMatrixToSvg(matrix);
  return (
    <svg
      className="totp-enroll-qr"
      viewBox={`0 0 ${size} ${size}`}
      width={size}
      height={size}
      role="img"
      aria-label="TOTP enrollment QR code — scan with your authenticator app, or use the provisioning URI or manual key below"
    >
      <rect width={size} height={size} fill="#ffffff" />
      <path d={path} fill="#000000" />
    </svg>
  );
}

export function TOTPEnrollmentControl({
  client,
  onEnrolled,
}: {
  readonly client: EshuApiClient;
  readonly onEnrolled: () => void;
}): React.JSX.Element {
  const [phase, setPhase] = useState<TOTPEnrollmentPhase>("idle");
  const [begin, setBegin] = useState<TOTPBeginResult | null>(null);
  const [code, setCode] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleBegin(): Promise<void> {
    setBusy(true);
    setMessage(null);
    try {
      const result = await beginTOTPEnrollment(client);
      setBegin(result);
      setPhase("confirming");
    } catch (err) {
      setMessage(err instanceof Error ? err.message : "Failed to start authenticator app setup.");
    } finally {
      setBusy(false);
    }
  }

  async function handleConfirm(): Promise<void> {
    if (!begin) return;
    setBusy(true);
    setMessage(null);
    try {
      const result = await confirmTOTPEnrollment(client, begin.factor_id, code);
      if (result.status === "activated") {
        setPhase("activated");
        onEnrolled();
      } else if (result.status === "invalid_code") {
        setMessage("That code did not match. Check the time on your device and try again.");
      } else {
        setMessage(result.message);
      }
    } finally {
      setBusy(false);
    }
  }

  if (phase === "activated") {
    return (
      <div className="totp-enroll">
        <p className="totp-enroll-success">Authenticator app enabled.</p>
      </div>
    );
  }

  if (phase === "idle") {
    return (
      <div className="totp-enroll">
        {message ? <p className="totp-enroll-error">{message}</p> : null}
        <button
          type="button"
          className="totp-enroll-start"
          disabled={busy}
          onClick={() => void handleBegin()}
        >
          {busy ? "Starting…" : "Set up authenticator app"}
        </button>
      </div>
    );
  }

  return (
    <div className="totp-enroll">
      <p>Scan or paste this into your authenticator app, or enter the key manually.</p>
      {begin?.otpauth_uri ? <TOTPEnrollmentQr otpauthUri={begin.otpauth_uri} /> : null}
      <label htmlFor="totp-uri">Provisioning URI</label>
      <input
        id="totp-uri"
        type="text"
        readOnly
        value={begin?.otpauth_uri ?? ""}
        onFocus={(e) => e.currentTarget.select()}
      />
      <label htmlFor="totp-secret">Manual entry key</label>
      <input
        id="totp-secret"
        type="text"
        readOnly
        value={begin?.secret ?? ""}
        onFocus={(e) => e.currentTarget.select()}
      />
      <label htmlFor="totp-code">Enter the 6-digit code from your app</label>
      <input
        id="totp-code"
        type="text"
        inputMode="numeric"
        autoComplete="one-time-code"
        value={code}
        disabled={busy}
        onChange={(e) => setCode(e.target.value)}
      />
      {message ? <p className="totp-enroll-error">{message}</p> : null}
      <button
        type="button"
        disabled={busy || code.trim().length === 0}
        onClick={() => void handleConfirm()}
      >
        {busy ? "Verifying…" : "Activate"}
      </button>
    </div>
  );
}
