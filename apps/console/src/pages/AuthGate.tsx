// AuthGate.tsx — routes between LoginPage and SetupPage (#4965).
// App.tsx renders LoginPage unconditionally whenever no session exists
// (source.status === "needs-connection"); this component preserves that
// exact contract while adding one branch: check GET /api/v0/auth/setup-state
// first, and render SetupPage instead of LoginPage while needs_setup is
// true. Extracted into its own file (rather than growing App.tsx, which is
// already at the 500-line cap) so App.tsx's diff stays a one-line component
// swap.
//
// SetupPage is loaded via React.lazy (mirroring WorkspacePage in
// appRoutes.tsx): it only ever renders on a fresh, un-provisioned
// deployment, so its code (recovery-code icons, the stepper) must not grow
// the main bundle every established deployment ships on every load — the
// console:bundle-budget gate enforces this.
import { lazy, Suspense, useEffect, useState } from "react";

import { LoginPage, type LoginPageProps } from "./LoginPage";
import { getSetupState } from "../api/setupSession";

const SetupPage = lazy(() =>
  import("./SetupPage").then((module) => ({ default: module.SetupPage })),
);

export type AuthGateProps = LoginPageProps;

export function AuthGate(props: AuthGateProps): React.JSX.Element {
  // null = still checking; true/false once GET /api/v0/auth/setup-state
  // answers. A failed check assumes false (fall back to the existing login
  // surface — never surface an unexpected wizard on a transient error).
  const [needsSetup, setNeedsSetup] = useState<boolean | null>(null);

  useEffect(() => {
    let cancelled = false;
    getSetupState(props.client)
      .then((state) => {
        if (!cancelled) setNeedsSetup(state.needs_setup);
      })
      .catch((err: unknown) => {
        console.warn("[eshu] GET /api/v0/auth/setup-state failed — falling back to login", err);
        if (!cancelled) setNeedsSetup(false);
      });
    return () => {
      cancelled = true;
    };
  }, [props.client]);

  if (needsSetup === null) {
    return <div className="login-page" aria-busy="true" />;
  }
  if (needsSetup) {
    return (
      <Suspense fallback={<div className="login-page" aria-busy="true" />}>
        <SetupPage client={props.client} onSuccess={props.onSuccess} />
      </Suspense>
    );
  }
  return <LoginPage {...props} />;
}
