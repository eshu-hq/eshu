// AnswerView.tsx — the rendered answer: truth label first, then prose or the
// evidence-only note, artifacts, limitations and the evidence expander.
import { ShieldCheck, TriangleAlert } from "lucide-react";
import type { RefObject } from "react";

import type { AskAnswer } from "../../api/askEshu";
import { Panel } from "../atoms";
import { ArtifactCard } from "./ArtifactCard";
import { EvidenceList } from "./EvidenceList";
import { renderMarkdown } from "./markdown";
import { TruthBadge } from "./TruthBadge";

/** The full answer panel. Leads with the truth badge; usable with no prose. */
export function AnswerView({
  answer,
  headingRef
}: {
  readonly answer: AskAnswer;
  readonly headingRef?: RefObject<HTMLHeadingElement | null>;
}): React.JSX.Element {
  const prose = answer.answer_prose;
  const evidenceOnly = prose.length === 0;
  const hasLimitations = answer.limitations.length > 0 || answer.partial;
  return (
    <Panel className="answer-panel">
      <div className="answer-head">
        <h3 ref={headingRef} tabIndex={-1}>
          Answer
        </h3>
        <TruthBadge big level={answer.truth_class} />
      </div>

      {hasLimitations ? (
        <div className="partial-banner" role="note">
          <TriangleAlert aria-hidden size={15} />
          <div>
            <strong>{answer.partial ? "This answer is partial." : "This answer has limitations."}</strong>
            <span> Eshu is showing what it could verify — don&apos;t read it as complete.</span>
          </div>
        </div>
      ) : null}

      {evidenceOnly ? (
        <div className="evidence-only-note">
          <ShieldCheck aria-hidden size={15} /> Narrated answers are off — showing the evidence Eshu gathered. The
          reasoning trace and artifacts below are the answer.
        </div>
      ) : (
        <div className="answer-prose">{renderMarkdown(prose)}</div>
      )}

      {answer.artifacts.length > 0 ? (
        <div className="artifact-stack">
          {answer.artifacts.map((artifact, index) => (
            <ArtifactCard artifact={artifact} key={index} />
          ))}
        </div>
      ) : null}

      {answer.limitations.length > 0 ? (
        <div className="limits-box">
          <div className="section-label">Limitations</div>
          <ul className="limits-list">
            {answer.limitations.map((limitation, index) => (
              <li key={index}>
                <i />
                {limitation}
              </li>
            ))}
          </ul>
        </div>
      ) : null}

      <EvidenceList handles={answer.evidence_handles} />
    </Panel>
  );
}
