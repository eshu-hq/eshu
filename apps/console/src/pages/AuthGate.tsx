// AuthGate.tsx — routes between LoginPage and SetupPage (#4965).
// App.tsx renders LoginPage unconditionally whenever no session exists
// (source.status === "needs-connection"); this component preserves that
// exact contract while adding one branch: check GET /api/v0/auth/setup-state
// first, and render SetupPage instead of LoginPage while needs_setup is
// true. Extracted into its own file (rather than growing App.tsx, which is
// already at the 500-line cap) so App.tsx's diff stays a one-line component
// swap.
//
// Both LoginPage and SetupPage are loaded via React.lazy (mirroring
// WorkspacePage in appRoutes.tsx): AuthGate never renders both, and neither
// is needed by any authenticated session, so their restyled markup and icon
// imports must not grow the main bundle every already-provisioned
// deployment ships on every load — the console:bundle-budget gate enforces
// this. The chunk fetch overlaps the setup-state network round trip this
// component already makes, so it adds no perceptible extra latency.
import { lazy, Suspense, useEffect, useState } from "react";

import type { LoginPageProps } from "./LoginPage";
import { getSetupState } from "../api/setupSession";

const LoginPage = lazy(() =>
  import("./LoginPage").then((module) => ({ default: module.LoginPage })),
);
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
  return (
    <Suspense fallback={<div className="login-page" aria-busy="true" />}>
      {needsSetup ? (
        <SetupPage client={props.client} onSuccess={props.onSuccess} />
      ) : (
        <LoginPage {...props} />
      )}
    </Suspense>
  );
}
