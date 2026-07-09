// pages/admin/CopyField.tsx
// A read-only, copy-to-clipboard field for the values an operator must
// register with their IdP (OIDC redirect URI, SAML SP entity id / ACS URL —
// #4967). Mirrors ArtifactCard's copy affordance
// (apps/console/src/components/ask/ArtifactCard.tsx): same lucide-react
// Copy/Check icons, same "copied" timeout pattern, so this reads as the same
// console rather than a bespoke widget for one panel.
import { Check, Copy } from "lucide-react";
import { useState } from "react";

export function CopyField({
  label,
  value,
  ariaLabel,
}: {
  readonly label: string;
  readonly value: string;
  readonly ariaLabel: string;
}): React.JSX.Element {
  const [copied, setCopied] = useState(false);

  async function onCopy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {
      // Clipboard access can be denied; the field remains selectable/readable.
    }
  }

  return (
    <div className="provider-field provider-field-readonly">
      <span>{label}</span>
      <div className="field-copy-row">
        <input value={value} readOnly aria-label={ariaLabel} />
        <button type="button" className="btn-ghost field-copy-btn" onClick={() => void onCopy()}>
          {copied ? <Check size={14} /> : <Copy size={14} />}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
    </div>
  );
}
