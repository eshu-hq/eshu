import { ArrowRight, CheckCircle2, Server, Users } from "lucide-react";
import { useState } from "react";

import { siteContent } from "./siteContent";
import type { RoleScenario } from "./siteContent";

/** Interactive "Every Role, Same Source of Truth" section tying CTO, Support, Dev, and SecOps together. */
export function RoleShowcaseSection(): React.JSX.Element {
  const [selectedRole, setSelectedRole] = useState<RoleScenario>(siteContent.roleScenarios[0]);

  return (
    <section className="site-section roles-section" id="roles" aria-labelledby="roles-title">
      <div className="section-heading">
        <span className="section-kicker">Organizational knowledge base</span>
        <h2 id="roles-title">Every role asks the same living graph</h2>
        <p>
          Tribal knowledge walks out the door and wikis go stale within days. Eshu builds
          institutional memory directly from your code, infrastructure, security, and runtime
          artifacts — so every team and assistant works from the same evidence.
        </p>
      </div>

      <div className="pill-tabs" role="tablist" aria-label="Organizational roles">
        {siteContent.roleScenarios.map((scenario) => {
          const active = scenario.id === selectedRole.id;
          return (
            <button
              key={scenario.id}
              type="button"
              role="tab"
              aria-selected={active}
              className={active ? "pill-tab is-active" : "pill-tab"}
              onClick={() => setSelectedRole(scenario)}
            >
              {scenario.role}
            </button>
          );
        })}
      </div>

      <div className="role-stage" role="tabpanel">
        <div className="role-stage-left">
          <div className="role-meta-row">
            <span className="role-context">
              <Users aria-hidden="true" />
              {selectedRole.context}
            </span>
            <span className="chip-tag">{selectedRole.interfaceLabel}</span>
          </div>
          <blockquote className="role-prompt">&ldquo;{selectedRole.prompt}&rdquo;</blockquote>
          <p className="role-answer">{selectedRole.answerSummary}</p>
        </div>

        <div className="role-evidence-card">
          <div className="role-evidence-head">
            <span>Observed evidence returned</span>
          </div>
          <ul className="role-evidence-list">
            {selectedRole.evidencePoints.map((point) => (
              <li key={point}>
                <CheckCircle2 aria-hidden="true" />
                <code>{point}</code>
              </li>
            ))}
          </ul>
        </div>
      </div>
    </section>
  );
}

/** Fused Architecture Pipeline + Incremental Rollout + Day-2 Operations chapter. */
export function RolloutAndOpsSection(): React.JSX.Element {
  return (
    <section className="site-section rollout-section" id="deploy" aria-labelledby="deploy-title">
      <div className="section-heading">
        <span className="section-kicker">Architecture &amp; Day-1 to Month-1 rollout</span>
        <h2 id="deploy-title">How it works — and how teams roll it out safely</h2>
        <p>
          Collectors never write directly to the graph. Versioned facts land in Postgres first so
          every correlation can be retried, audited, and rebuilt. Start with Git repositories alone
          on Day 1, then opt into collectors as your team grows.
        </p>
      </div>

      <ol className="pipeline-flow" aria-label="Facts-first architecture pipeline">
        {siteContent.pipeline.map((step, index) => (
          <li key={step.step}>
            <div className="pipeline-step-head">
              <span className="pipeline-step-num">{step.step}</span>
              {index < siteContent.pipeline.length - 1 ? (
                <ArrowRight className="pipeline-arrow" aria-hidden="true" />
              ) : null}
            </div>
            <h3>{step.label}</h3>
            <p>{step.detail}</p>
          </li>
        ))}
      </ol>

      <div className="rollout-grid">
        {siteContent.rolloutPhases.map((item) => (
          <article className="rollout-card" key={item.phase}>
            <div className="rollout-card-top">
              <span className="rollout-phase">{item.phase}</span>
              <span className="rollout-time">{item.timeframe}</span>
            </div>
            <h3>{item.title}</h3>
            <p className="rollout-req">{item.requirements}</p>
            <ul className="rollout-list">
              {item.unlocks.map((unlock) => (
                <li key={unlock}>{unlock}</li>
              ))}
            </ul>
          </article>
        ))}
      </div>

      <div className="ops-grid" aria-label="Deployment and Day-2 operations">
        {siteContent.opsFacts.map((fact) => (
          <article className="ops-card" key={fact.title}>
            <div className="ops-card-head">
              <Server aria-hidden="true" />
              <h3>{fact.title}</h3>
            </div>
            <p>{fact.detail}</p>
          </article>
        ))}
      </div>
    </section>
  );
}
