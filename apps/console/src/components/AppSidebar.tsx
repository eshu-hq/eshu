// components/AppSidebar.tsx
// The left navigation rail: brand mark, capability-gated nav groups, and the
// backend status card. Extracted out of App.tsx (which sits at the console's
// 500-line file cap — see CLAUDE.md) so nav rendering has its own
// single-purpose module, mirroring the appRoutes.tsx extraction that keeps
// the route table out of App.tsx for the same reason. Pure relocation: no
// rendered-output change from the inline block it replaces.
import { NavLink } from "react-router-dom";

import type { SourceState } from "./SourceControls";
import type { ConsoleModel } from "../console/types";
import { NAV_GROUPS } from "../i18n/navigation";
import { FormattedMessage, useConsoleIntl } from "../i18n/provider";
import { formatRepositoryCount, shellMessageDescriptors } from "../i18n/shellMessages";

export interface AppSidebarProps {
  readonly allowedNav: ReadonlySet<string>;
  // visibleModel drives nav item counts (verified-evidence-only filter
  // applies here); model (unfiltered) drives the backend status card.
  readonly visibleModel: ConsoleModel;
  readonly model: ConsoleModel;
  readonly source: SourceState;
  readonly backendMode: string;
}

export function AppSidebar({
  allowedNav,
  visibleModel,
  model,
  source,
  backendMode,
}: AppSidebarProps): React.JSX.Element {
  const intl = useConsoleIntl();
  return (
    <nav className="sidebar">
      <a className="brand" href="/">
        <span className="brand-mark brand-glyph" aria-hidden>
          <i />
          <i />
          <i />
        </span>
        <span>
          <span className="brand-name">
            e<b>shu</b>
          </span>
          <span className="brand-sub">
            <FormattedMessage {...shellMessageDescriptors.brandSubtitle} />
          </span>
        </span>
      </a>
      {NAV_GROUPS.map((group) => (
        <div className="nav-section" key={group.messageId}>
          <div className="nav-group-label">
            <FormattedMessage id={group.messageId} />
          </div>
          {group.items
            .filter((n) => allowedNav.has(n.to))
            .map((n) => {
              const Icon = n.icon;
              const count = n.count?.(visibleModel) ?? null;
              const label = intl.formatMessage({ id: n.messageId });
              return (
                <NavLink
                  key={n.to}
                  to={n.to}
                  aria-label={label}
                  className={({ isActive }) => `nav-item${isActive ? " active" : ""}`}
                >
                  <Icon aria-hidden />
                  <span className="nav-label">{label}</span>
                  {count !== null ? (
                    <span aria-hidden className={`nav-count${n.alert ? " alert" : ""}`}>
                      {count}
                    </span>
                  ) : null}
                </NavLink>
              );
            })}
        </div>
      ))}
      <div className="sidebar-foot">
        <div className="backend-card">
          <div className="bc-top">
            <i />
            {model.runtime.indexStatus}
          </div>
          <div className="bc-meta">
            <span>{backendMode}</span>
            <span>
              {source.status === "connected"
                ? formatRepositoryCount(intl, model.runtime.repositories)
                : "—"}
            </span>
          </div>
        </div>
      </div>
    </nav>
  );
}
