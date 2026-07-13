// components/useConfirm.tsx
// Accessible, non-blocking, promise-based confirmation dialog for admin
// mutations. Replaces native window.confirm(), which blocks the renderer main
// thread (freezing the tab under headless/automation drivers that do not
// auto-dismiss the native modal) and cannot be styled, focus-managed, or
// exercised by component tests.
//
// Usage:
//   const { confirm, confirmDialog } = useConfirm();
//   ...
//   if (!(await confirm("Delete X?", { danger: true }))) return;
//   ...
//   return <Panel>{confirmDialog}{...}</Panel>;
//
// The returned promise resolves true when the user confirms, false when they
// cancel (Cancel button, scrim click, or Escape). Rendering confirmDialog is
// required for confirm() to surface UI; it is null while idle.
import { useCallback, useEffect, useId, useRef, useState } from "react";

export interface ConfirmOptions {
  // danger styles the confirm button as destructive (revoke/delete/disable).
  readonly danger?: boolean;
  // confirmLabel overrides the confirm button text (default "Confirm").
  readonly confirmLabel?: string;
}

interface PendingConfirm {
  readonly message: string;
  readonly options: ConfirmOptions;
  readonly resolve: (confirmed: boolean) => void;
}

export interface UseConfirm {
  readonly confirm: (message: string, options?: ConfirmOptions) => Promise<boolean>;
  readonly confirmDialog: React.JSX.Element | null;
}

export function useConfirm(): UseConfirm {
  const [pending, setPending] = useState<PendingConfirm | null>(null);
  const confirmRef = useRef<HTMLButtonElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const messageId = useId();

  const confirm = useCallback(
    (message: string, options: ConfirmOptions = {}) =>
      new Promise<boolean>((resolve) => {
        setPending((current) => {
          // If a dialog is already open, resolve its promise as cancelled so
          // the prior caller's await never hangs forever (re-entrancy safety
          // for panels that may open a second confirm before the first
          // settles).
          current?.resolve(false);
          return { message, options, resolve };
        });
      }),
    [],
  );

  // settle resolves the outstanding promise exactly once and closes the dialog.
  const settle = useCallback((confirmed: boolean) => {
    setPending((current) => {
      current?.resolve(confirmed);
      return null;
    });
  }, []);

  // Move focus to the confirm control when the dialog opens, matching the
  // drawer/EvidenceDrawer focus-on-open convention.
  useEffect(() => {
    if (pending) {
      confirmRef.current?.focus();
    }
  }, [pending]);

  // Escape cancels — window-level so it fires regardless of focus position.
  useEffect(() => {
    if (!pending) {
      return;
    }
    function onKey(event: globalThis.KeyboardEvent): void {
      if (event.key === "Escape") {
        settle(false);
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [pending, settle]);

  // Trap Tab focus inside the dialog while open (aria-modal hides the
  // background from assistive tech, so focus must never leave via Tab). Same
  // technique as ProviderConfigDrawer/EvidenceDrawer.
  function trapFocus(event: React.KeyboardEvent): void {
    if (event.key !== "Tab") {
      return;
    }
    const root = dialogRef.current;
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

  const confirmDialog = pending ? (
    <div className="confirm-scrim" onClick={() => settle(false)}>
      <div
        ref={dialogRef}
        className="confirm-dialog"
        role="alertdialog"
        aria-modal="true"
        aria-label="Confirm action"
        aria-describedby={messageId}
        onClick={(event) => event.stopPropagation()}
        onKeyDown={trapFocus}
      >
        <p id={messageId} className="confirm-dialog-message">
          {pending.message}
        </p>
        <div className="confirm-dialog-actions">
          <button type="button" className="btn-ghost" onClick={() => settle(false)}>
            Cancel
          </button>
          <button
            ref={confirmRef}
            type="button"
            className={pending.options.danger ? "btn-danger" : "btn-primary"}
            onClick={() => settle(true)}
          >
            {pending.options.confirmLabel ?? "Confirm"}
          </button>
        </div>
      </div>
    </div>
  ) : null;

  return { confirm, confirmDialog };
}
