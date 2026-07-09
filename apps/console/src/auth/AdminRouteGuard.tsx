// auth/AdminRouteGuard.tsx — wraps /admin (exact path) so a session without any
// ADMIN_ROUTE_FAMILIES permission family sees the 403 access screen instead
// of the Admin page shell (issue #4969). Fail-closed whenever the server
// reports permission_catalog_enforced; fail-open (today's #3703 contract)
// only when it does not. This is UX-only: every admin API route already
// enforces authorization server-side regardless of what this guard renders.
import type { ReactNode } from "react";

import { canAccessAdminRoute } from "./capabilityAccess";
import type { BrowserSessionAuth } from "../api/client";
import { AccessDeniedPage } from "../pages/AccessDeniedPage";

export function AdminRouteGuard({
  auth,
  children,
}: {
  readonly auth: BrowserSessionAuth | null | undefined;
  readonly children: ReactNode;
}): React.JSX.Element {
  if (canAccessAdminRoute(auth)) {
    return <>{children}</>;
  }
  console.warn(
    "[admin] /admin access denied — session lacks every identity_admin/roles_grants/tokens/audit_export family under an enforced permission catalog",
  );
  return <AccessDeniedPage />;
}
