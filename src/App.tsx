import { useState } from "react";
import {
  AlertTriangle,
  ArrowRight,
  Boxes,
  Cloud,
  Code,
  Menu,
  MessageCircle,
  Network,
  Route,
  SearchX,
  ShieldCheck,
  Terminal,
  Workflow,
  X
} from "lucide-react";
import { SourceRuntimeGraph } from "./SourceRuntimeGraph";
import { siteContent } from "./siteContent";
import type { CommandDemo, CleanupMode, PersonaDemo } from "./siteContent";

const capabilityIcons = [
  MessageCircle,
  ShieldCheck,
  Route,
  Workflow,
  AlertTriangle,
  Boxes,
  Code,
  Network,
  Cloud
] as const;

export function App(): React.JSX.Element {
  const [menuOpen, setMenuOpen] = useState(false);
  const [selectedCommand, setSelectedCommand] = useState<CommandDemo>(
    siteContent.commandDemos[0]
  );
  const [selectedPersona, setSelectedPersona] = useState<PersonaDemo>(
    siteContent.personaDemos[0]
  );
  const [selectedCleanupMode, setSelectedCleanupMode] = useState<CleanupMode>(
    siteContent.cleanupModes[0]
  );

  return (
    <div className="app-shell">
      <header className="site-header">
        <a className="brand-link" href="/" aria-label="Eshu home">
          <img src="/brand/eshu-logo-horizontal.svg" alt="Eshu" />
        </a>
        <button
          className="mobile-menu-button"
          type="button"
          aria-label={menuOpen ? "Close navigation" : "Open navigation"}
          aria-expanded={menuOpen}
          onClick={() => setMenuOpen((current) => !current)}
        >
          {menuOpen ? <X aria-hidden="true" /> : <Menu aria-hidden="true" />}
        </button>
        <nav className={menuOpen ? "site-nav is-open" : "site-nav"}>
          {siteContent.nav.map((item) => (
            <a key={item.label} href={item.href}>
              {item.label}
            </a>
          ))}
        </nav>
      </header>

      <main>
        <section className="hero-section">
          <div className="hero-copy">
            <div className="hero-logo-frame hero-logo-frame--full-preview">
              <img
                className="hero-logo"
                src="/brand/eshu-social-preview-1200x630.png"
                alt="Eshu display logo"
              />
            </div>
            <h1>{siteContent.hero.heading}</h1>
            <p className="hero-core-line">{siteContent.hero.coreLine}</p>
            <p>{siteContent.hero.description}</p>
            <div className="hero-actions" aria-label="Primary actions">
              <a className="button-primary" href={siteContent.hero.primaryCta.href}>
                {siteContent.hero.primaryCta.label}
                <ArrowRight aria-hidden="true" />
              </a>
              <a className="button-secondary" href={siteContent.hero.secondaryCta.href}>
                {siteContent.hero.secondaryCta.label}
              </a>
            </div>
          </div>
          <SourceRuntimeGraph selectedCommand={selectedCommand} />
        </section>

        <section
          className="whats-new-section"
          id="whats-new"
          aria-labelledby="whats-new-title"
        >
          <div className="section-heading">
            <h2 id="whats-new-title">What's new</h2>
            <p>
              The graph now has an answer layer, portable proof artifacts, and
              CI-backed claim checks. The launch story moved from "context" to
              evidence-backed action.
            </p>
          </div>
          <div className="whats-new-grid">
            {siteContent.whatsNew.map((item) => (
              <article className="whats-new-card" key={item.title}>
                <strong>{item.summary}</strong>
                <h3>{item.title}</h3>
                <p>{item.detail}</p>
              </article>
            ))}
          </div>
        </section>

        <section
          className="capabilities-section"
          id="product"
          aria-labelledby="capabilities-title"
        >
          <div className="section-heading">
            <h2 id="capabilities-title">What Eshu does</h2>
            <p>
              Eight surfaces plus one ask layer, each backed by real MCP tools,
              each with refusal-on-insufficient-evidence. One graph holds them all.
            </p>
          </div>
          <div className="capability-list">
            {siteContent.capabilities.map((capability, index) => {
              const Icon = capabilityIcons[index] ?? Network;
              return (
                <article className="capability-row" key={capability.title}>
                  <Icon aria-hidden="true" />
                  <h3>{capability.title}</h3>
                  <p>{capability.description}</p>
                </article>
              );
            })}
          </div>
        </section>

        <section
          className="pipeline-section"
          id="how-it-works"
          aria-labelledby="pipeline-title"
        >
          <div className="section-heading">
            <h2 id="pipeline-title">How it works</h2>
            <p>
              Eshu reads from the systems that already describe your stack,
              then turns their relationships into one graph with reducer-owned
              truth.
            </p>
          </div>
          <ol className="pipeline-flow">
            {siteContent.pipeline.map((step) => (
              <li key={step.label}>
                <span>{step.label}</span>
                <small>{step.detail}</small>
              </li>
            ))}
          </ol>
        </section>

        <section
          className="developer-section"
          id="cli"
          aria-labelledby="developer-title"
        >
          <div className="section-heading">
            <h2 id="developer-title">Run the graph</h2>
            <p>
              Click a CLI command or MCP tool and watch the answer change.
              Static demo, real output shapes — the workflow is the product
              shape.
            </p>
          </div>
          <div className="demo-workbench">
            <div className="command-rail" aria-label="Demo commands">
              {siteContent.commandDemos.map((demo) => (
                <button
                  aria-pressed={demo.command === selectedCommand.command}
                  className={
                    demo.command === selectedCommand.command
                      ? "command-button is-active"
                      : "command-button"
                  }
                  key={demo.command}
                  onClick={() => setSelectedCommand(demo)}
                  type="button"
                >
                  <Terminal aria-hidden="true" />
                  {demo.command}
                </button>
              ))}
            </div>
            <div className="terminal-card">
              <div className="terminal-title">
                <Terminal aria-hidden="true" />
                graph session
              </div>
              <p>{selectedCommand.summary}</p>
              <pre>{selectedCommand.output.map((line) => `$ ${line}`).join("\n")}</pre>
            </div>
          </div>
        </section>

        <section
          className="coverage-section"
          aria-labelledby="coverage-title"
        >
          <div className="section-heading">
            <h2 id="coverage-title">Code-to-cloud means more than code search</h2>
            <p>{siteContent.coverage}</p>
          </div>
        </section>

        <section className="proof-section" id="scale" aria-labelledby="proof-title">
          <div className="section-heading">
            <h2 id="proof-title">Built for the whole organization</h2>
            <p>
              Eshu covers the shared engineering estate — not just one repo,
              not just one team&apos;s local search problem.
            </p>
          </div>
          <div className="proof-grid">
            {siteContent.proofPoints.map((point) => (
              <article className="proof-card" key={point.title}>
                <strong>{point.value}</strong>
                <h3>{point.title}</h3>
                <p>{point.description}</p>
              </article>
            ))}
          </div>
        </section>

        <section
          className="surfaces-section"
          aria-labelledby="surfaces-title"
        >
          <div className="section-heading">
            <h2 id="surfaces-title">Where the graph shows up</h2>
            <p>
              A graph is only useful if every role in the org can reach it
              from the tools they already use.
            </p>
          </div>
          <div className="surface-grid">
            {siteContent.surfaces.map((surface) => (
              <article className="surface-card" key={surface.title}>
                <h3>{surface.title}</h3>
                <p>{surface.description}</p>
              </article>
            ))}
          </div>
        </section>

        <section
          className="personas-section"
          id="personas"
          aria-labelledby="personas-title"
        >
          <div className="section-heading">
            <h2 id="personas-title">Built for every engineering role</h2>
            <p>
              Same MCP server, same evidence graph, same truth envelope — for
              engineers, security, platform, leadership, Ask Eshu users, and
              customer-facing teams. Nine representative personas, full reference in the
              persona matrix.
            </p>
          </div>
          <div className="persona-tabs" aria-label="Role examples">
            {siteContent.personaDemos.map((persona) => (
              <button
                aria-pressed={persona.role === selectedPersona.role}
                className={
                  persona.role === selectedPersona.role
                    ? "persona-tab is-active"
                    : "persona-tab"
                }
                key={persona.role}
                onClick={() => setSelectedPersona(persona)}
                type="button"
              >
                {persona.role}
              </button>
            ))}
          </div>
          <article className="persona-answer">
            <p className="persona-context">{selectedPersona.context}</p>
            <h3>{selectedPersona.question}</h3>
            <p>{selectedPersona.answer}</p>
            <p className="persona-tool">
              <code>{selectedPersona.primaryTool}</code>
            </p>
          </article>
          <p className="persona-matrix-link">
            <a href={siteContent.references.fullPersonaMatrix}>
              Full persona × question × tool matrix (all 18 personas) →
            </a>
          </p>
        </section>

        <section
          className="cleanup-section"
          id="cleanup"
          aria-labelledby="cleanup-title"
        >
          <div className="section-heading">
            <h2 id="cleanup-title">Dead code, dead IaC, and unmanaged resources</h2>
            <p>
              The same reachability model that finds unused code paths also
              finds Terraform that no live workload depends on — and AWS
              resources that exist without a Terraform module at all.
            </p>
          </div>
          <div className="cleanup-toggle" aria-label="Cleanup mode">
            {siteContent.cleanupModes.map((mode) => (
              <button
                aria-pressed={mode.label === selectedCleanupMode.label}
                className={
                  mode.label === selectedCleanupMode.label
                    ? "cleanup-button is-active"
                    : "cleanup-button"
                }
                key={mode.label}
                onClick={() => setSelectedCleanupMode(mode)}
                type="button"
              >
                {mode.label}
              </button>
            ))}
          </div>
          <article className="cleanup-panel">
            <h3>{selectedCleanupMode.summary}</h3>
            <ul>
              {selectedCleanupMode.findings.map((finding) => (
                <li key={finding}>{finding}</li>
              ))}
            </ul>
          </article>
        </section>

        <section
          className="difference-section"
          aria-labelledby="difference-title"
        >
          <div className="section-heading">
            <h2 id="difference-title">{siteContent.difference.heading}</h2>
          </div>
          <div className="difference-grid">
            {siteContent.difference.points.map((point) => (
              <article className="difference-card" key={point.target}>
                <strong>{point.target}</strong>
                <p>{point.claim}</p>
              </article>
            ))}
          </div>
        </section>

        <section
          className="tryit-section"
          id="try-it"
          aria-labelledby="try-it-title"
        >
          <div className="section-heading">
            <h2 id="try-it-title">{siteContent.tryIt.heading}</h2>
          </div>
          <ol className="tryit-steps">
            {siteContent.tryIt.steps.map((step) => (
              <li key={step}>
                <code>{step}</code>
              </li>
            ))}
          </ol>
          <p className="tryit-first-question">{siteContent.tryIt.firstQuestion}</p>
          <div className="hero-actions" aria-label="Get started">
            <a className="button-primary" href={siteContent.tryIt.ctaHref}>
              {siteContent.tryIt.ctaLabel}
              <ArrowRight aria-hidden="true" />
            </a>
          </div>
        </section>

        <section
          className="prompts-section"
          aria-labelledby="prompts-title"
        >
          <div className="section-heading">
            <h2 id="prompts-title">First prompts by role</h2>
            <p>
              Drop one of these into Claude Code, Codex, Cursor, or VS Code.
              Eshu answers with provenance.
            </p>
          </div>
          <div className="prompt-grid">
            {siteContent.rolePrompts.map((item) => (
              <article className="prompt-card" key={item.role}>
                <h3>{item.role}</h3>
                <p>{item.prompt}</p>
              </article>
            ))}
          </div>
        </section>

        <section
          className="use-cases-section"
          id="use-cases"
          aria-labelledby="use-cases-title"
        >
          <div className="section-heading">
            <h2 id="use-cases-title">Questions Eshu answers</h2>
            <p>
              These are the questions teams ask during refactors, incidents,
              audits, migrations, and customer demos.
            </p>
          </div>
          <div className="use-case-grid">
            {siteContent.useCases.map((useCase) => (
              <article className="use-case-card" key={useCase.question}>
                <h3>{useCase.question}</h3>
                <p>{useCase.answer}</p>
              </article>
            ))}
          </div>
        </section>

        <section
          className="references-section"
          aria-labelledby="references-title"
        >
          <div className="section-heading">
            <h2 id="references-title">Read more</h2>
          </div>
          <ul className="references-list">
            <li>
              <a href={siteContent.references.fullPersonaMatrix}>
                Persona × question × tool matrix →
              </a>{" "}
              all 18 personas, first questions, MCP tools.
            </li>
            <li>
              <a href={siteContent.references.supplyChainDemo}>
                Supply chain CVE-to-impact demo runbook →
              </a>{" "}
              runnable synthetic demo with refusal variant.
            </li>
            <li>
              <a href={siteContent.references.replatformingDemo}>
                AWS → Azure re-platforming demo runbook →
              </a>{" "}
              compose_replatforming_plan to LLM-generated Terraform.
            </li>
            <li>
              <a href={siteContent.references.lightweightAudit}>
                Local lightweight capability audit →
              </a>{" "}
              what refuses vs degrades per capability.
            </li>
          </ul>
        </section>

        <section className="closing-section" aria-labelledby="closing-title">
          <img src="/brand/eshu-icon.svg" alt="" aria-hidden="true" />
          <h2 id="closing-title">{siteContent.closing.heading}</h2>
          <p>{siteContent.closing.description}</p>
          <div className="hero-actions" aria-label="Closing actions">
            <a className="button-primary" href={siteContent.hero.primaryCta.href}>
              {siteContent.hero.primaryCta.label}
              <ArrowRight aria-hidden="true" />
            </a>
            <a className="button-secondary" href={siteContent.hero.secondaryCta.href}>
              {siteContent.hero.secondaryCta.label}
            </a>
          </div>
        </section>
      </main>
    </div>
  );
}
