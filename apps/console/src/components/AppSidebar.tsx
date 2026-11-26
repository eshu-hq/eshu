// components/AppSidebar.tsx
// The left navigation rail: brand mark, capability-gated nav groups, and the
// backend status card. Extracted out of App.tsx (which sits at the console's
// 500-line file cap — see AGENTS.md) so nav rendering has its own
// single-purpose module, mirroring the appRoutes.tsx extraction that keeps
// the route table out of App.tsx for the same reason. Pure relocation: no
// rendered-output change from the inline block it replaces.
import { ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";
import { NavLink, useLocation } from "react-router";

import type { SourceState } from "./SourceControls";
import type { ConsoleModel } from "../console/types";
import { NAV_GROUPS } from "../i18n/navigation";
import { FormattedMessage, useConsoleIntl } from "../i18n/provider";
import { formatRepositoryCount, shellMessageDescriptors } from "../i18n/shellMessages";
import "./appSidebar.css";

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
  const { pathname } = useLocation();
  const activeGroup = groupForPath(pathname);
  // The Start group and the group holding the current page are open; the rest
  // collapse so the rail reads as a handful of jobs instead of 40 links.
  const [openGroups, setOpenGroups] = useState<ReadonlySet<string>>(
    () => new Set([START_GROUP, activeGroup].filter((id): id is string => id !== null)),
  );
  useEffect(() => {
    if (activeGroup === null) return;
    setOpenGroups((current) =>
      current.has(activeGroup) ? current : new Set([...current, activeGroup]),
    );
  }, [activeGroup]);
  function toggleGroup(id: string): void {
    setOpenGroups((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }
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
      {NAV_GROUPS.map((group) => {
        const items = group.items.filter((n) => allowedNav.has(n.to));
        if (items.length === 0) return null;
        const expanded = openGroups.has(group.messageId);
        const panelId = `nav-${group.messageId.replace(/\./g, "-")}`;
        return (
          <div className={`nav-section${expanded ? " open" : ""}`} key={group.messageId}>
            <button
              type="button"
              className="nav-group-label nav-group-toggle"
              aria-expanded={expanded}
              aria-controls={panelId}
              onClick={() => toggleGroup(group.messageId)}
            >
              <ChevronRight aria-hidden className="nav-group-chev" />
              <FormattedMessage id={group.messageId} />
            </button>
            <div id={panelId} hidden={!expanded}>
              {items.map((n) => {
                const Icon = n.icon;
                const count = n.count?.(visibleModel) ?? null;
                const label = intl.formatMessage({ id: n.messageId });
                return (
                  <NavLink
                    key={n.to}
                    to={n.to}
                    end={n.to === "/"}
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
          </div>
        );
      })}
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

const START_GROUP = NAV_GROUPS[0]?.messageId ?? "";

// groupForPath finds the nav group whose item best matches the current path
// (longest prefix), so deep links like /vulnerabilities/CVE-1 open Security.
function groupForPath(pathname: string): string | null {
  let best: { readonly group: string; readonly length: number } | null = null;
  for (const group of NAV_GROUPS) {
    for (const item of group.items) {
      const matches =
        item.to === "/"
          ? pathname === "/"
          : pathname === item.to || pathname.startsWith(`${item.to}/`);
      if (matches && (best === null || item.to.length > best.length)) {
        best = { group: group.messageId, length: item.to.length };
      }
    }
  }
  return best?.group ?? null;
}
