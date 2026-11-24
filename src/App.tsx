import { ArrowRight, ArrowUpRight, Check, Copy, Menu, Moon, Sun, X } from "lucide-react";
import { useEffect, useState } from "react";

import {
  CapabilityExplorerSection,
  CollectorEcosystemSection,
  CommandWorkbenchSection,
  QuickstartSection,
  SurfacesSection,
} from "./PlatformSections";
import { RoleShowcaseSection, RolloutAndOpsSection } from "./RoleAndRolloutSections";
import { siteContent } from "./siteContent";
import type { HeroTrace } from "./siteContent";
import { SourceRuntimeGraph } from "./SourceRuntimeGraph";

type SiteTheme = "dark" | "light";

export function App(): React.JSX.Element {
  const [menuOpen, setMenuOpen] = useState(false);
  const [theme, setTheme] = useState<SiteTheme>("dark");
  const [selectedTrace, setSelectedTrace] = useState<HeroTrace>(siteContent.heroTraces[0]);
  const [heroCopied, setHeroCopied] = useState(false);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", theme);
  }, [theme]);

  const handleCopyHeroCommand = (): void => {
    if (typeof navigator !== "undefined" && navigator.clipboard) {
      void navigator.clipboard.writeText(siteContent.hero.copyCommand).catch(() => undefined);
    }
    setHeroCopied(true);
    window.setTimeout(() => setHeroCopied(false), 1600);
  };

  return (
    <div className="app-shell">
      <header className="site-header">
        <div className="header-inner">
          <a className="brand-link" href="/" aria-label="Eshu home">
            <img className="brand-mark" src="/brand/eshu-icon.svg" alt="Eshu" />
            <span className="brand-wordmark">eshu</span>
            <span className="brand-badge">MIT</span>
          </a>

          <nav
            className={menuOpen ? "site-nav is-open" : "site-nav"}
            aria-label="Primary navigation"
          >
            {siteContent.nav.map((item) => (
              <a key={item.label} href={item.href} onClick={() => setMenuOpen(false)}>
                {item.label}
              </a>
            ))}
          </nav>

          <div className="header-actions">
            <button
              type="button"
              className="theme-toggle"
              aria-label={
                theme === "dark" ? "Switch to warm light theme" : "Switch to dark obsidian theme"
              }
              onClick={() => setTheme((current) => (current === "dark" ? "light" : "dark"))}
            >
              {theme === "dark" ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
            </button>
            <a className="header-github" href={siteContent.links.github}>
              GitHub
              <ArrowUpRight aria-hidden="true" />
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
          </div>
        </div>
      </header>

      <main>
        <div className="band band--hero">
          <section className="hero-section">
            <div className="hero-copy">
              <p className="hero-eyebrow">{siteContent.hero.eyebrow}</p>
              <h1>{siteContent.hero.heading}</h1>
              <p className="hero-description">{siteContent.hero.description}</p>

              <div className="hero-actions" aria-label="Primary actions">
                <a className="button-primary" href={siteContent.hero.primaryCta.href}>
                  {siteContent.hero.primaryCta.label}
                  <ArrowRight aria-hidden="true" />
                </a>
                <a className="button-secondary" href={siteContent.hero.secondaryCta.href}>
                  {siteContent.hero.secondaryCta.label}
                </a>
              </div>

              <div className="hero-cli-bar">
                <button type="button" className="command-copy-pill" onClick={handleCopyHeroCommand}>
                  <span className="pill-prompt" aria-hidden="true">
                    $
                  </span>
                  <code>{siteContent.hero.copyCommand}</code>
                  <span className="pill-copy-state">
                    {heroCopied ? (
                      <>
                        <Check aria-hidden="true" />
                        Copied
                      </>
                    ) : (
                      <>
                        <Copy aria-hidden="true" />
                        Copy demo command
                      </>
                    )}
                  </span>
                </button>
                <p className="hero-footnote">{siteContent.hero.footnote}</p>
              </div>
            </div>

            <SourceRuntimeGraph selectedTrace={selectedTrace} onSelectTrace={setSelectedTrace} />
          </section>
        </div>

        <div className="band band--elevated">
          <section className="metrics-strip" aria-label="Verified platform scale">
            <div className="metrics-grid">
              {siteContent.metrics.map((stat) => (
                <div className="metric-cell" key={stat.label}>
                  <strong>{stat.value}</strong>
                  <span>{stat.label}</span>
                  <small>{stat.detail}</small>
                </div>
              ))}
            </div>
          </section>
        </div>

        <div className="band band--base">
          <RoleShowcaseSection />
        </div>

        <div className="band band--elevated">
          <CapabilityExplorerSection />
        </div>

        <div className="band band--base">
          <CollectorEcosystemSection />
        </div>

        <div className="band band--elevated">
          <RolloutAndOpsSection />
        </div>

        <div className="band band--base">
          <CommandWorkbenchSection />
        </div>

        <div className="band band--elevated">
          <SurfacesSection />
        </div>

        <div className="band band--base">
          <section
            className="site-section principles-strip"
            id="principles"
            aria-labelledby="principles-title"
          >
            <div className="principles-head">
              <span className="section-kicker">Engineering guarantees</span>
              <h2 id="principles-title">Built for proof, not hype</h2>
            </div>

            <div className="principles-editorial">
              {siteContent.principles.map((item, idx) => (
                <article className="principle-col" key={item.title}>
                  <span className="principle-index">0{idx + 1}</span>
                  <h3>{item.title}</h3>
                  <p>{item.description}</p>
                </article>
              ))}
            </div>
          </section>

          <QuickstartSection />

          <section className="closing-section" aria-labelledby="closing-title">
            <div className="closing-card">
              <img className="closing-mark" src="/brand/eshu-icon.svg" alt="" aria-hidden="true" />
              <h2 id="closing-title">{siteContent.closing.heading}</h2>
              <p>{siteContent.closing.description}</p>
              <div className="closing-actions" aria-label="Closing actions">
                <a className="button-primary" href={siteContent.hero.primaryCta.href}>
                  {siteContent.hero.primaryCta.label}
                  <ArrowRight aria-hidden="true" />
                </a>
                <a className="button-secondary" href={siteContent.hero.secondaryCta.href}>
                  {siteContent.hero.secondaryCta.label}
                </a>
                <a className="button-secondary" href={siteContent.links.github}>
                  View on GitHub
                  <ArrowUpRight aria-hidden="true" />
                </a>
              </div>
            </div>
          </section>
        </div>
      </main>

      <footer className="site-footer">
        <div className="footer-inner">
          <div className="footer-brand">
            <div className="footer-brand-row">
              <img className="brand-mark" src="/brand/eshu-icon.svg" alt="" aria-hidden="true" />
              <span className="brand-wordmark">eshu</span>
            </div>
            <p>
              Self-hosted organizational knowledge graph connecting code, dependencies, supply
              chain, infrastructure, and runtime. MIT-licensed.
            </p>
          </div>

          <div className="footer-columns">
            {siteContent.footer.map((group) => (
              <div className="footer-column" key={group.heading}>
                <h3>{group.heading}</h3>
                <ul>
                  {group.links.map((link) => (
                    <li key={link.label}>
                      <a href={link.href}>{link.label}</a>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </div>
        </div>
      </footer>
    </div>
  );
}
