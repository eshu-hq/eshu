import { ArrowUpRight, Check, Copy, Layers, Lock, Shield, Terminal } from "lucide-react";
import { useState } from "react";

import { siteContent } from "./siteContent";
import type {
  CapabilityTab,
  CollectorCategory,
  CollectorReadiness,
  CommandDemo,
  QuickstartTab,
} from "./siteContent";

const readinessLabels: Record<CollectorReadiness, string> = {
  implemented: "Implemented",
  partial: "Partial",
  gated: "Opt-in / Gated",
  foundation_only: "Foundation",
};

function copyText(value: string, onCopied: () => void): void {
  if (typeof navigator !== "undefined" && navigator.clipboard) {
    void navigator.clipboard.writeText(value).catch(() => undefined);
  }
  onCopied();
}

/** Interactive 6-pillar capability explorer with single-row desktop tabs. */
export function CapabilityExplorerSection(): React.JSX.Element {
  const [activeTab, setActiveTab] = useState<CapabilityTab>(siteContent.capabilityTabs[0]);

  return (
    <section
      className="site-section capabilities-section"
      id="capabilities"
      aria-labelledby="capabilities-title"
    >
      <div className="section-heading">
        <span className="section-kicker">162 verified capabilities</span>
        <h2 id="capabilities-title">One platform across every engineering layer</h2>
        <p>
          Explore what Eshu indexes and proves across code, supply chain, cloud exposure, IaC drift,
          incidents, observability metadata, and documentation truth.
        </p>
      </div>

      <div
        className="pill-tabs pill-tabs--single-row"
        role="tablist"
        aria-label="Capability pillars"
      >
        {siteContent.capabilityTabs.map((tab) => {
          const active = tab.id === activeTab.id;
          return (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={active}
              className={active ? "pill-tab is-active" : "pill-tab"}
              onClick={() => setActiveTab(tab)}
            >
              {tab.label}
            </button>
          );
        })}
      </div>

      <div className="capability-stage" role="tabpanel">
        <div className="capability-stage-intro">
          <h3>{activeTab.headline}</h3>
          <p>{activeTab.summary}</p>
        </div>
        <div className="capability-grid">
          {activeTab.highlights.map((item) => (
            <article className="capability-card" key={item.title}>
              <div className="capability-card-top">
                <h4>{item.title}</h4>
                {item.tag ? <span className="chip-tag">{item.tag}</span> : null}
              </div>
              <p>{item.description}</p>
            </article>
          ))}
        </div>
      </div>
    </section>
  );
}

/** Compact 4-column desktop integration matrix for all 23 read-only collectors. */
export function CollectorEcosystemSection(): React.JSX.Element {
  const [selectedCategory, setSelectedCategory] = useState<"All" | CollectorCategory>("All");
  const [showAllCollectors, setShowAllCollectors] = useState(false);

  const filteredCollectors =
    selectedCategory === "All"
      ? siteContent.collectors
      : siteContent.collectors.filter((item) => item.category === selectedCategory);

  const isCollapsedAll =
    selectedCategory === "All" && !showAllCollectors && filteredCollectors.length > 8;
  const visibleCollectors = isCollapsedAll ? filteredCollectors.slice(0, 8) : filteredCollectors;

  const countForCategory = (category: "All" | CollectorCategory): number =>
    category === "All"
      ? siteContent.collectors.length
      : siteContent.collectors.filter((item) => item.category === category).length;

  return (
    <section
      className="site-section collectors-section"
      id="collectors"
      aria-labelledby="collectors-title"
    >
      <div className="section-heading section-heading--split">
        <div>
          <span className="section-kicker">23 collector lanes · 31 org repos</span>
          <h2 id="collectors-title">Read-only collectors, off by default</h2>
          <p>
            Collectors only observe and emit typed facts into Postgres — only the reducer writes
            graph truth. Every status badge below is checked automatically against code and proof
            gates.
          </p>
        </div>
        <a className="inline-doc-link" href={siteContent.links.readiness}>
          Collector readiness contract
          <ArrowUpRight aria-hidden="true" />
        </a>
      </div>

      <div className="readiness-legend" aria-label="Collector readiness legend">
        {siteContent.readinessLegend.map((item) => (
          <div className="legend-item" key={item.key}>
            <span className={`readiness-badge readiness-badge--${item.key}`}>{item.label}</span>
            <span>{item.meaning}</span>
          </div>
        ))}
      </div>

      <div className="pill-tabs" role="tablist" aria-label="Collector families">
        {siteContent.collectorCategories.map((category) => {
          const active = category === selectedCategory;
          return (
            <button
              key={category}
              type="button"
              role="tab"
              aria-selected={active}
              className={active ? "pill-tab is-active" : "pill-tab"}
              onClick={() => setSelectedCategory(category)}
            >
              {category} ({countForCategory(category)})
            </button>
          );
        })}
      </div>

      <div className="collector-grid">
        {visibleCollectors.map((collector) => (
          <article className="collector-card" key={collector.lane}>
            <div className="collector-card-head">
              <div>
                <h3>{collector.name}</h3>
                <code className="collector-lane">{collector.lane}</code>
              </div>
              <span className={`readiness-badge readiness-badge--${collector.readiness}`}>
                {readinessLabels[collector.readiness]}
              </span>
            </div>
            <p>{collector.summary}</p>
            {collector.privacyNote ? (
              <div className="collector-privacy">
                <Lock aria-hidden="true" />
                <span>{collector.privacyNote}</span>
              </div>
            ) : null}
          </article>
        ))}
      </div>

      {selectedCategory === "All" ? (
        <div className="collector-expand-row">
          <button
            type="button"
            className="button-secondary"
            onClick={() => setShowAllCollectors((current) => !current)}
          >
            {showAllCollectors
              ? "Show fewer collectors"
              : `Show all ${siteContent.collectors.length} collector lanes`}
          </button>
        </div>
      ) : null}
    </section>
  );
}

/** Interactive CLI, MCP, and HTTP API command workbench with clean output formatting. */
export function CommandWorkbenchSection(): React.JSX.Element {
  const [selectedDemo, setSelectedDemo] = useState<CommandDemo>(siteContent.commandDemos[0]);
  const [copiedId, setCopiedId] = useState<string | null>(null);

  return (
    <section
      className="site-section workbench-section"
      id="workbench"
      aria-labelledby="workbench-title"
    >
      <div className="section-heading">
        <span className="section-kicker">Real command &amp; envelope shapes</span>
        <h2 id="workbench-title">Inspect what Eshu actually returns</h2>
        <p>
          Every CLI command, MCP tool, and HTTP endpoint returns bounded answers with truth classes,
          freshness, and explicit missing-evidence slots.
        </p>
      </div>

      <div className="demo-workbench">
        <div className="command-rail" aria-label="Demo commands">
          {siteContent.commandDemos.map((demo) => {
            const active = demo.id === selectedDemo.id;
            return (
              <button
                key={demo.id}
                type="button"
                aria-pressed={active}
                className={active ? "command-button is-active" : "command-button"}
                onClick={() => setSelectedDemo(demo)}
              >
                <span className="command-surface-badge">{demo.surface}</span>
                <span className="command-text">
                  <Terminal aria-hidden="true" />
                  {demo.command}
                </span>
              </button>
            );
          })}
        </div>

        <div className="terminal-card">
          <div className="terminal-head">
            <div className="terminal-title">
              <Terminal aria-hidden="true" />
              <code>{selectedDemo.command}</code>
            </div>
            <button
              type="button"
              className="copy-mini-button"
              onClick={() => {
                copyText(selectedDemo.command, () => {
                  setCopiedId(selectedDemo.id);
                  window.setTimeout(() => setCopiedId(null), 1600);
                });
              }}
            >
              {copiedId === selectedDemo.id ? (
                <>
                  <Check aria-hidden="true" />
                  Copied
                </>
              ) : (
                <>
                  <Copy aria-hidden="true" />
                  Copy command
                </>
              )}
            </button>
          </div>
          <p className="terminal-summary">{selectedDemo.summary}</p>
          <pre className="terminal-output">
            <code>{`$ ${selectedDemo.command}\n\n${selectedDemo.output.join("\n")}`}</code>
          </pre>
        </div>
      </div>
    </section>
  );
}

/** Balanced 2-row asymmetric Bento grid for all five surfaces (zero orphan empty hole). */
export function SurfacesSection(): React.JSX.Element {
  const [mcpSurface, consoleSurface, ...secondarySurfaces] = siteContent.surfaces;

  return (
    <section
      className="site-section surfaces-section"
      id="surfaces"
      aria-labelledby="surfaces-title"
    >
      <div className="section-heading">
        <span className="section-kicker">One truth model, five interfaces</span>
        <h2 id="surfaces-title">Built for terminals, coding agents, APIs, and teams</h2>
        <p>
          Use Eshu from your IDE assistant, CI pipeline, browser console, or custom Go collectors —
          with enterprise authentication and governance included under MIT.
        </p>
      </div>

      <div className="surface-bento">
        {mcpSurface ? (
          <article className="surface-card surface-card--mcp">
            <div className="surface-card-top">
              <Layers aria-hidden="true" />
              <span className="surface-metric">{mcpSurface.metric}</span>
            </div>
            <h3>{mcpSurface.title}</h3>
            <p>{mcpSurface.description}</p>
            <div className="bento-mini-visual" aria-label="MCP router context savings">
              <div className="router-row">
                <span className="router-pill">full</span>
                <code>173 tools advertised directly</code>
              </div>
              <div className="router-row router-row--highlight">
                <span className="router-pill router-pill--teal">router</span>
                <code>find_tool + call_tool · minimal context tokens</code>
              </div>
              <div className="router-row">
                <span className="router-pill">structured</span>
                <code>Single-copy JSON payload for Claude &amp; Codex</code>
              </div>
            </div>
            <ul className="surface-bullets">
              {mcpSurface.bullets.map((bullet) => (
                <li key={bullet}>{bullet}</li>
              ))}
            </ul>
          </article>
        ) : null}

        {consoleSurface ? (
          <article className="surface-card surface-card--console">
            <div className="surface-card-top">
              <Shield aria-hidden="true" />
              <span className="surface-metric">{consoleSurface.metric}</span>
            </div>
            <h3>{consoleSurface.title}</h3>
            <p>{consoleSurface.description}</p>
            <div className="console-mini-frame" aria-label="Self-hosted console preview">
              <div className="console-mini-bar">
                <span />
                <span />
                <span />
                <code>eshu console · read-only</code>
              </div>
              <div className="console-mini-body">
                <div className="console-mini-nav">
                  <span>Service Atlas</span>
                  <span className="is-active">Exposure Path</span>
                  <span>Reachable Vulns</span>
                  <span>Ops Live Board</span>
                </div>
                <div className="console-mini-badges">
                  <span>OIDC / SAML</span>
                  <span>GitHub SSO</span>
                  <span>RBAC &amp; MFA</span>
                  <span>Audit Logs</span>
                </div>
              </div>
            </div>
            <ul className="surface-bullets">
              {consoleSurface.bullets.map((bullet) => (
                <li key={bullet}>{bullet}</li>
              ))}
            </ul>
          </article>
        ) : null}

        {secondarySurfaces.map((surface) => (
          <article className="surface-card surface-card--compact" key={surface.title}>
            <div className="surface-card-top">
              <Layers aria-hidden="true" />
              <span className="surface-metric">{surface.metric}</span>
            </div>
            <h3>{surface.title}</h3>
            <p>{surface.description}</p>
            <ul className="surface-bullets">
              {surface.bullets.map((bullet) => (
                <li key={bullet}>{bullet}</li>
              ))}
            </ul>
          </article>
        ))}
      </div>
    </section>
  );
}

/** Accurate, tabbed quickstart covering demo mode, eshu-deploy, and MCP setup. */
export function QuickstartSection(): React.JSX.Element {
  const [activeQuickstart, setActiveQuickstart] = useState<QuickstartTab>(
    siteContent.quickstartTabs[0],
  );
  const [copiedQuickstart, setCopiedQuickstart] = useState(false);

  const snippet = activeQuickstart.commands.join("\n");

  return (
    <section className="site-section tryit-section" id="try-it" aria-labelledby="try-it-title">
      <div className="section-heading">
        <span className="section-kicker">Self-hosted quickstart</span>
        <h2 id="try-it-title">Run Eshu on your laptop or cluster</h2>
        <p>
          Start with a zero-credential synthetic organization via <code>eshu demo up</code>, stand
          up the full Compose or Helm stack from <code>eshu-deploy</code>, or connect your MCP
          coding assistant.
        </p>
      </div>

      <div className="pill-tabs" role="tablist" aria-label="Quickstart paths">
        {siteContent.quickstartTabs.map((tab) => {
          const active = tab.id === activeQuickstart.id;
          return (
            <button
              key={tab.id}
              type="button"
              role="tab"
              aria-selected={active}
              className={active ? "pill-tab is-active" : "pill-tab"}
              onClick={() => {
                setActiveQuickstart(tab);
                setCopiedQuickstart(false);
              }}
            >
              {tab.label}
            </button>
          );
        })}
      </div>

      <div className="quickstart-card" role="tabpanel">
        <div className="quickstart-head">
          <div>
            <div className="quickstart-meta-row">
              <span className="chip-tag">{activeQuickstart.badge}</span>
              <span className="quickstart-prereq">{activeQuickstart.prerequisites}</span>
            </div>
            <p className="quickstart-desc">{activeQuickstart.description}</p>
          </div>
          <button
            type="button"
            className="copy-mini-button"
            onClick={() => {
              copyText(snippet, () => {
                setCopiedQuickstart(true);
                window.setTimeout(() => setCopiedQuickstart(false), 1600);
              });
            }}
          >
            {copiedQuickstart ? (
              <>
                <Check aria-hidden="true" />
                Copied
              </>
            ) : (
              <>
                <Copy aria-hidden="true" />
                Copy commands
              </>
            )}
          </button>
        </div>

        <pre className="terminal-output quickstart-code">
          <code>{snippet}</code>
        </pre>

        <p className="quickstart-note">{activeQuickstart.note}</p>
      </div>
    </section>
  );
}
