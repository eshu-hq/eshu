// pages/AccessDeniedPage.tsx — the 403 access screen the /admin route guard
// renders instead of the Admin page shell when the session lacks every
// permission family that gates an Admin panel (issue #4969). This is a
// UX-only signal: the server enforces authorization on every admin API
// route regardless of what this screen shows or hides.
import { ShieldAlert } from "lucide-react";
import { useEffect, useRef } from "react";
import { Link } from "react-router-dom";

import { ADMIN_ROUTE_FAMILIES } from "../auth/capabilityAccess";
import { Badge, Panel } from "../components/atoms";
import "./accessDenied.css";

export function AccessDeniedPage(): React.JSX.Element {
  // Move focus to the denial heading on mount so keyboard/screen-reader users
  // land on the 403 message immediately after a route-level redirect instead
  // of staying wherever focus was (often the document body), which silently
  // strands assistive-tech users on the previous page's context (issue #4996).
  const headingRef = useRef<HTMLHeadingElement>(null);
  useEffect(() => {
    headingRef.current?.focus();
  }, []);

  return (
    <section className="page-shell">
      <div className="access-denied-wrap" role="alert" aria-labelledby="access-denied-title">
        <Panel className="access-denied-panel">
          <div className="access-denied-icon" aria-hidden>
            <ShieldAlert />
          </div>
          <h1 id="access-denied-title" ref={headingRef} tabIndex={-1}>
            You don't have access to this area
          </h1>
          <p>
            This area needs one of the identity, roles, tokens, or audit admin permissions, and your
            current session doesn't hold any of them.
          </p>
          <div
            className="access-denied-families"
            aria-label="Permission families that unlock this area"
          >
            {ADMIN_ROUTE_FAMILIES.map((family) => (
              <Badge key={family} tone="neutral">
                {family}
              </Badge>
            ))}
          </div>
          <p className="access-denied-hint">
            See <Link to="/profile">your effective permissions</Link> to check which roles and
            permission families your session currently holds, or ask an admin to grant one of the
            families above.
          </p>
        </Panel>
      </div>
    </section>
  );
}
