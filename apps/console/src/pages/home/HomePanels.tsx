// pages/home/HomePanels.tsx
// The panels under the Home hero. Each panel owns its loading, unavailable and
// empty states so one slow or forbidden read never blanks the page.
import {
  ArrowRight,
  BookOpen,
  Boxes,
  Cloud,
  Compass,
  FolderGit2,
  ShieldCheck,
  Users,
  type LucideIcon,
} from "lucide-react";
import { Link } from "react-router";

import { coverageAreas, teams, unavailableSentence, type AttentionItem } from "./homeModel";
import type { CatalogWire } from "../../api/contracts/wire/CatalogWire";
import type { EcosystemOverviewWire } from "../../api/contracts/wire/EcosystemOverviewWire";
import type { InvestigationWorkflowsWire } from "../../api/contracts/wire/InvestigationWorkflowsWire";
import type { StatusCollectorReadinessWire } from "../../api/contracts/wire/StatusCollectorReadinessWire";
import type { Section } from "../../api/knowledgeHome";

export function Panel(props: {
  readonly id: string;
  readonly title: string;
  readonly subtitle?: string;
  readonly children: React.ReactNode;
  readonly action?: { readonly to: string; readonly label: string };
}): React.JSX.Element {
  return (
    <section className="kh-panel" aria-labelledby={`${props.id}-title`}>
      <header className="kh-panel-head">
        <div>
          <h3 id={`${props.id}-title`}>{props.title}</h3>
          {props.subtitle ? <p>{props.subtitle}</p> : null}
        </div>
        {props.action ? (
          <Link className="kh-link" to={props.action.to}>
            {props.action.label} <ArrowRight aria-hidden />
          </Link>
        ) : null}
      </header>
      {props.children}
    </section>
  );
}

// SectionBody renders a section's non-ready states and hands ready data to render.
export function SectionBody<T>(props: {
  readonly section: Section<T>;
  readonly label: string;
  readonly render: (data: T) => React.ReactNode;
}): React.JSX.Element {
  const { section } = props;
  if (section.status === "loading") {
    return (
      <div className="kh-skeleton" aria-busy="true" aria-live="polite">
        <span className="kh-sr">Loading {props.label}…</span>
        <i />
        <i />
        <i />
      </div>
    );
  }
  if (section.status === "unavailable") {
    return (
      <p className="kh-unavailable" role="note">
        {unavailableSentence(section.reason)}
      </p>
    );
  }
  return <>{props.render(section.data)}</>;
}

const TONE_LABEL = { critical: "Urgent", warning: "Needs a look", info: "FYI" } as const;

export function AttentionPanel({
  items,
}: {
  readonly items: readonly AttentionItem[];
}): React.JSX.Element {
  return (
    <Panel
      id="kh-attention"
      title="Needs attention"
      subtitle="The most important things Eshu has noticed, in plain language."
    >
      {items.length === 0 ? (
        <p className="kh-empty">Nothing needs attention right now.</p>
      ) : (
        <ul className="kh-attention">
          {items.map((item) => (
            <li key={item.id} className={`kh-att ${item.tone}`}>
              <span className="kh-tone">{TONE_LABEL[item.tone]}</span>
              <div className="kh-att-body">
                <strong>{item.title}</strong>
                <p>{item.detail}</p>
              </div>
              <Link className="kh-link" to={item.href}>
                {item.action} <ArrowRight aria-hidden />
              </Link>
            </li>
          ))}
        </ul>
      )}
    </Panel>
  );
}

interface BrowseTile {
  readonly to: string;
  readonly icon: LucideIcon;
  readonly title: string;
  readonly text: string;
  readonly count: number | null;
}

export function BrowsePanel({
  overview,
}: {
  readonly overview: EcosystemOverviewWire | null;
}): React.JSX.Element {
  const tiles: readonly BrowseTile[] = [
    {
      to: "/catalog",
      icon: Boxes,
      title: "Services",
      text: "Everything we run, who owns it and where it runs.",
      count: overview?.workload_count ?? null,
    },
    {
      to: "/repositories",
      icon: FolderGit2,
      title: "Code",
      text: "Every repository, what's in it and what it powers.",
      count: overview?.repo_count ?? null,
    },
    {
      to: "/cloud",
      icon: Cloud,
      title: "Infrastructure",
      text: "Cloud resources, clusters and how they connect.",
      count: overview?.instance_count ?? null,
    },
    {
      to: "/vulnerabilities",
      icon: ShieldCheck,
      title: "Security",
      text: "Vulnerabilities that actually affect running software.",
      count: null,
    },
    {
      to: "/semantic-search",
      icon: BookOpen,
      title: "Docs & runbooks",
      text: "Search documentation and see which pages are still true.",
      count: null,
    },
    {
      to: "/guided-questions",
      icon: Compass,
      title: "Guided investigations",
      text: "Step-by-step help for common questions.",
      count: null,
    },
  ];
  return (
    <Panel
      id="kh-browse"
      title="Browse your organization"
      subtitle="Not sure what to search for? Start here."
    >
      <ul className="kh-tiles">
        {tiles.map((tile) => {
          const Icon = tile.icon;
          return (
            <li key={tile.to}>
              <Link className="kh-tile" to={tile.to}>
                <Icon aria-hidden />
                <span className="kh-tile-title">
                  {tile.title}
                  {tile.count !== null ? <em>{tile.count.toLocaleString("en-US")}</em> : null}
                </span>
                <span className="kh-tile-text">{tile.text}</span>
              </Link>
            </li>
          );
        })}
      </ul>
    </Panel>
  );
}

export function TeamsPanel({ catalog }: { readonly catalog: CatalogWire }): React.JSX.Element {
  const rows = teams(catalog);
  const listed = catalog.services?.length ?? 0;
  return rows.length === 0 ? (
    <p className="kh-empty">No ownership has been declared in your service catalog yet.</p>
  ) : (
    <>
      {catalog.truncated ? (
        <p className="kh-note">
          Based on the first {listed.toLocaleString("en-US")} services in the catalog.
        </p>
      ) : null}
      <ul className="kh-teams">
        {rows.map((team) => (
          <li key={team.name}>
            <Users aria-hidden />
            <div>
              <strong>{team.name}</strong>
              <span>
                {team.services.length} {team.services.length === 1 ? "service" : "services"}
                {team.critical > 0 ? ` · ${team.critical} business-critical` : ""}
              </span>
              <p>
                {team.services.slice(0, 4).join(", ")}
                {team.services.length > 4 ? "…" : ""}
              </p>
            </div>
          </li>
        ))}
      </ul>
    </>
  );
}

const AREA_STATE = {
  connected: "Connected",
  partial: "Partly connected",
  missing: "Not connected",
} as const;

export function CoverageList({
  collectors,
}: {
  readonly collectors: StatusCollectorReadinessWire;
}): React.JSX.Element {
  return (
    <ul className="kh-coverage">
      {coverageAreas(collectors).map((area) => (
        <li key={area.id} className={`kh-area ${area.state}`}>
          <details>
            <summary>
              <span className="kh-area-dot" aria-hidden />
              <span className="kh-area-name">{area.name}</span>
              <span className="kh-area-state">{AREA_STATE[area.state]}</span>
            </summary>
            <p>Lets you answer: {area.answers}.</p>
            {area.sources.length === 0 ? (
              <p>No data source is set up for this yet.</p>
            ) : (
              <ul>
                {area.sources.map((source) => (
                  <li key={source.name}>
                    <strong>{source.name}</strong> —{" "}
                    {source.state === "implemented" ? "working" : source.state}
                    {source.state !== "implemented" ? <span> · {source.next}</span> : null}
                  </li>
                ))}
              </ul>
            )}
          </details>
        </li>
      ))}
    </ul>
  );
}

export function GuidedList({
  workflows,
}: {
  readonly workflows: InvestigationWorkflowsWire;
}): React.JSX.Element {
  const rows = workflows.workflows ?? [];
  return rows.length === 0 ? (
    <p className="kh-empty">No guided investigations are published on this deployment.</p>
  ) : (
    <ul className="kh-guided">
      {rows.map((wf) => (
        <li key={wf.id}>
          <strong>{wf.name}</strong>
          <p>{wf.description}</p>
        </li>
      ))}
    </ul>
  );
}

export function TruthGuide(): React.JSX.Element {
  return (
    <details className="kh-truth">
      <summary>How to read Eshu's answers</summary>
      <dl>
        <dt>
          <span className="kh-chip exact">exact</span>
        </dt>
        <dd>Proven directly from a source file, manifest or cloud record.</dd>
        <dt>
          <span className="kh-chip derived">derived</span>
        </dt>
        <dd>Worked out by connecting several sources. Reliable, but one step removed.</dd>
        <dt>
          <span className="kh-chip partial">partial</span>
        </dt>
        <dd>Some evidence is missing. Eshu says what's missing instead of guessing.</dd>
      </dl>
    </details>
  );
}

export type { Section };
