// pages/admin/ProviderConfigDrawer.tsx
// Add/Edit drawer for a DB-backed identity provider config (#4967, consumes
// the #4966 CRUD API). Never used for an env-managed row — AdminProvidersPanel
// disables the Edit action for managed_by==="environment" rows before this
// component can be opened, so this drawer only ever writes to a
// database-owned row.
//
// Flow: fill the form -> "Run test sign-in" saves the current fields as a
// draft (create on first use, update thereafter) and calls test-connection ->
// "Save" saves the current fields and, only when the immediately-preceding
// test passed for these exact fields, also enables the provider; otherwise it
// leaves it as a draft. Editing any field after a test invalidates that test
// result (testResult resets to null) so a stale pass can never drive Save
// straight to "enabled" — the actual enable-safety compare-and-swap is
// enforced server-side (EnableProviderConfig's expectedActiveRevisionID
// guard); this client-side reset is UX only.
//
// Focus/keyboard behavior mirrors EvidenceDrawer.tsx exactly (close-button
// focus on mount, Escape closes, Tab is trapped inside the drawer) so this
// drawer behaves identically to every other drawer in the console.
import { useEffect, useRef, useState } from "react";

import { OidcProviderFields } from "./OidcProviderFields";
import {
  emptyOidcForm,
  emptySamlForm,
  oidcFormFromExisting,
  samlFormFromExisting,
  buildOidcInput,
  buildSamlInput,
  oidcFormValid,
  samlFormValid,
} from "./providerConfigForm";
import type { OidcFormState, SamlFormState } from "./providerConfigForm";
import { SamlProviderFields } from "./SamlProviderFields";
import type { AdminProviderConfigItem, ProviderConfigInput } from "../../api/adminProviderConfig";
import {
  createProviderConfig,
  updateProviderConfig,
  enableProviderConfig,
  testProviderConfigConnection,
  newClientProviderConfigId,
  oidcRedirectUri,
  samlAcsUrl,
  samlServiceProviderEntityId,
  toFormKind,
} from "../../api/adminProviderConfig";
import type { EshuApiClient } from "../../api/client";
import { Badge } from "../../components/atoms";

export function ProviderConfigDrawer({
  client,
  baseUrl,
  existing,
  onClose,
  onSaved,
}: {
  readonly client: EshuApiClient;
  readonly baseUrl: string;
  readonly existing?: AdminProviderConfigItem;
  readonly onClose: () => void;
  readonly onSaved: () => void;
}): React.JSX.Element {
  const [kind, setKind] = useState<"oidc" | "saml">(
    existing ? toFormKind(existing.provider_kind) : "oidc",
  );
  const [providerConfigId] = useState(
    () => existing?.provider_config_id ?? newClientProviderConfigId(),
  );
  const [exists, setExists] = useState(Boolean(existing));
  const [status, setStatus] = useState(existing?.status ?? "draft");
  const [oidcForm, setOidcForm] = useState<OidcFormState>(
    existing && toFormKind(existing.provider_kind) === "oidc"
      ? oidcFormFromExisting(existing)
      : emptyOidcForm,
  );
  const [samlForm, setSamlForm] = useState<SamlFormState>(
    existing && toFormKind(existing.provider_kind) === "saml"
      ? samlFormFromExisting(existing)
      : emptySamlForm,
  );
  const [testResult, setTestResult] = useState<{ ok: boolean; detail?: string } | null>(null);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const closeRef = useRef<HTMLButtonElement>(null);
  const drawerRef = useRef<HTMLElement>(null);

  // Focus the close control once on open, matching EvidenceDrawer.tsx.
  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  useEffect(() => {
    function onKey(event: globalThis.KeyboardEvent): void {
      if (event.key === "Escape") {
        onClose();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  // Trap Tab focus inside the drawer — see EvidenceDrawer.tsx's identical
  // trapFocus for why this is needed (aria-modal hides the background from
  // assistive tech, so focus must never leave the drawer via Tab).
  function trapFocus(event: React.KeyboardEvent): void {
    if (event.key !== "Tab") {
      return;
    }
    const root = drawerRef.current;
    if (root === null) {
      return;
    }
    const focusables = root.querySelectorAll<HTMLElement>(
      'a[href], button:not([disabled]), input:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    );
    if (focusables.length === 0) {
      return;
    }
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = root.ownerDocument.activeElement;
    if (event.shiftKey && active === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && active === last) {
      event.preventDefault();
      first.focus();
    }
  }

  const valid = kind === "oidc" ? oidcFormValid(oidcForm) : samlFormValid(samlForm);

  function currentInput(): ProviderConfigInput {
    return kind === "oidc"
      ? buildOidcInput(oidcForm, providerConfigId, baseUrl)
      : buildSamlInput(samlForm, providerConfigId, baseUrl);
  }

  async function saveDraft(): Promise<boolean> {
    const input = currentInput();
    const outcome = exists
      ? await updateProviderConfig(client, providerConfigId, input)
      : await createProviderConfig(client, input);
    if (!outcome.ok) {
      setNotice(outcome.errorMessage ?? "Failed to save the provider config.");
      return false;
    }
    setExists(true);
    // Every successful create/update always leaves the provider in "draft"
    // status server-side — the backend's activateProviderConfigActiveRevisionQuery
    // resets status to 'draft' unconditionally whenever the active revision
    // changes, even when updating a provider that was previously active (see
    // go/internal/storage/postgres/identity_provider_config_writes_sql.go's
    // doc comment: an already-active provider whose revision just changed
    // must go back through Enable's test-gate before it can be trusted for
    // login again). UpdateProviderConfig's write result reports the
    // PRE-transaction status rather than this fresh value, so this sets
    // "draft" directly instead of trusting outcome.result.status here — only
    // enableProviderConfig's result (below) is a reliable post-write status.
    setStatus("draft");
    return true;
  }

  async function onRunTest(): Promise<void> {
    setTesting(true);
    setNotice(null);
    setTestResult(null);
    const saved = await saveDraft();
    if (!saved) {
      setTesting(false);
      return;
    }
    const result = await testProviderConfigConnection(client, providerConfigId);
    // testResult drives the dedicated pass/fail indicator below the fields
    // (not the transient `notice` line, which is reserved for save/enable
    // outcomes) — see the "Test sign-in passed/failed" block in the render.
    setTestResult({ ok: result.ok, detail: result.detail });
    setTesting(false);
    onSaved();
  }

  async function onSave(): Promise<void> {
    setSaving(true);
    setNotice(null);
    const saved = await saveDraft();
    if (!saved) {
      setSaving(false);
      return;
    }
    if (testResult?.ok) {
      const enableOutcome = await enableProviderConfig(client, providerConfigId);
      if (enableOutcome.ok) {
        setStatus(enableOutcome.result?.status ?? "active");
        setNotice("Saved and enabled — the SSO button will appear on the login page.");
      } else {
        setNotice(
          enableOutcome.errorMessage ??
            "Saved as draft — enabling failed. Run a test sign-in again and retry.",
        );
      }
    } else {
      setNotice("Saved as draft. Run a test sign-in before this provider can be enabled.");
    }
    setSaving(false);
    onSaved();
  }

  const redirectUri = oidcRedirectUri(baseUrl);
  const acsUrl = samlAcsUrl(baseUrl, providerConfigId);
  const spEntityId = samlServiceProviderEntityId(baseUrl, providerConfigId);
  const busy = saving || testing;

  return (
    <>
      <div className="drawer-scrim" onClick={onClose} />
      <aside
        ref={drawerRef}
        className="drawer"
        role="dialog"
        aria-modal="true"
        aria-label={existing ? "Edit provider" : "Add provider"}
        onKeyDown={trapFocus}
      >
        <div className="drawer-head">
          <div>
            <div className="insp-kind">{existing ? "Edit provider" : "Add provider"}</div>
            <strong>
              <Badge tone={status === "active" ? "teal" : "neutral"}>{status}</Badge>
            </strong>
          </div>
          <button
            ref={closeRef}
            className="drawer-close"
            onClick={onClose}
            aria-label="Close"
            type="button"
          >
            ✕
          </button>
        </div>
        <div className="drawer-body">
          {existing ? null : (
            <div className="provider-kind-toggle" role="radiogroup" aria-label="Provider kind">
              <button
                type="button"
                className={`btn-ghost${kind === "oidc" ? " active" : ""}`}
                disabled={busy}
                onClick={() => setKind("oidc")}
              >
                OIDC
              </button>
              <button
                type="button"
                className={`btn-ghost${kind === "saml" ? " active" : ""}`}
                disabled={busy}
                onClick={() => setKind("saml")}
              >
                SAML
              </button>
            </div>
          )}

          {kind === "oidc" ? (
            <OidcProviderFields
              form={oidcForm}
              redirectUri={redirectUri}
              disabled={busy}
              onChange={(next) => {
                setOidcForm(next);
                setTestResult(null);
              }}
            />
          ) : (
            <SamlProviderFields
              form={samlForm}
              serviceProviderEntityId={spEntityId}
              acsUrl={acsUrl}
              disabled={busy}
              onChange={(next) => {
                setSamlForm(next);
                setTestResult(null);
              }}
            />
          )}

          {testResult ? (
            <p className="empty-note provider-test-result" role="status">
              <Badge tone={testResult.ok ? "teal" : "crit"}>
                {testResult.ok ? "Test sign-in passed" : "Test sign-in failed"}
              </Badge>
              {testResult.detail ? ` ${testResult.detail}` : ""}
            </p>
          ) : null}

          {notice ? (
            <p className="empty-note" role="status">
              {notice}
            </p>
          ) : null}

          <div className="row wrap" style={{ gap: 8 }}>
            <button
              type="button"
              className="btn-ghost"
              disabled={!valid || busy}
              onClick={() => void onRunTest()}
            >
              {testing ? "Testing…" : "Run test sign-in"}
            </button>
            <button
              type="button"
              className="btn-ghost active"
              disabled={!valid || busy}
              onClick={() => void onSave()}
            >
              {saving ? "Saving…" : "Save"}
            </button>
          </div>
        </div>
      </aside>
    </>
  );
}
