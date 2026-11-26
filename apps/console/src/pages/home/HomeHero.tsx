// pages/home/HomeHero.tsx
// The top of Home: what Eshu knows, whether it's current, and one box to find
// or ask anything. The role lens only changes which questions are suggested.
import { ArrowRight, CircleCheck, CircleAlert, Search, Sparkles } from "lucide-react";
import { useId, useState, type FormEvent } from "react";
import { Link } from "react-router";

import type { Health, RoleId } from "./homeModel";
import { ROLE_LENSES } from "./roles";

export interface HomeHeroProps {
  readonly summary: string | null;
  readonly health: Health;
  readonly role: RoleId;
  readonly blurb: string;
  readonly questions: readonly string[];
  readonly onRoleChange: (role: RoleId) => void;
  readonly onAsk: (question: string) => void;
  readonly onFind: (query: string) => void;
}

// looksLikeQuestion routes "who owns checkout?" to Ask and "checkout" to search.
export function looksLikeQuestion(text: string): boolean {
  const trimmed = text.trim().toLowerCase();
  return (
    trimmed.endsWith("?") ||
    /^(who|what|where|when|why|how|which|is|are|does|do|can|should|show|list|explain|tell)\b/.test(trimmed)
  );
}

export function HomeHero(props: HomeHeroProps): React.JSX.Element {
  const [text, setText] = useState("");
  const inputId = useId();
  const helpId = useId();

  function submit(event: FormEvent<HTMLFormElement>): void {
    event.preventDefault();
    const value = text.trim();
    if (value.length === 0) return;
    if (looksLikeQuestion(value)) props.onAsk(value);
    else props.onFind(value);
  }

  return (
    <section className="kh-hero" aria-labelledby="kh-hero-title">
      <div className="kh-hero-top">
        <div>
          <h2 id="kh-hero-title">What do you want to know?</h2>
          <p className="kh-summary">{props.summary ?? "Eshu is your organization's knowledge base for code, services, infrastructure and the people who own them."}</p>
        </div>
        <HealthPill health={props.health} />
      </div>

      <form className="kh-ask" role="search" onSubmit={submit}>
        <label className="kh-sr" htmlFor={inputId}>
          Search or ask Eshu
        </label>
        <Search aria-hidden className="kh-ask-icon" />
        <input
          id={inputId}
          aria-describedby={helpId}
          autoComplete="off"
          placeholder="Search for a service, team or repo — or ask a question"
          value={text}
          onChange={(event) => setText(event.target.value)}
        />
        <button className="kh-ask-submit" type="submit">
          {looksLikeQuestion(text) ? "Ask" : "Search"} <ArrowRight aria-hidden />
        </button>
      </form>
      <p className="kh-hint" id={helpId}>
        Type a name to jump to it, or ask in plain English — every answer shows where it came from.
      </p>

      <fieldset className="kh-roles">
        <legend>Show suggestions for</legend>
        {ROLE_LENSES.map((lens) => (
          <label key={lens.id} className={`kh-role${props.role === lens.id ? " on" : ""}`}>
            <input
              checked={props.role === lens.id}
              name="kh-role"
              type="radio"
              value={lens.id}
              onChange={() => props.onRoleChange(lens.id)}
            />
            {lens.label}
          </label>
        ))}
      </fieldset>

      <div className="kh-questions">
        <p className="kh-role-blurb">{props.blurb}</p>
        <ul aria-label="Suggested questions">
          {props.questions.map((question) => (
            <li key={question}>
              <button className="kh-question" type="button" onClick={() => props.onAsk(question)}>
                <Sparkles aria-hidden />
                <span>{question}</span>
              </button>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

function HealthPill({ health }: { readonly health: Health }): React.JSX.Element | null {
  if (health.state === "unknown") return null;
  if (health.state === "healthy") {
    return (
      <p className="kh-health ok" role="status">
        <CircleCheck aria-hidden /> {health.summary}
      </p>
    );
  }
  return (
    <details className="kh-health warn">
      <summary>
        <CircleAlert aria-hidden /> {health.summary}
      </summary>
      <ul>
        {health.notes.map((note) => (
          <li key={note}>{note}</li>
        ))}
        <li>
          <Link to="/freshness-causality">See why answers are behind</Link>
        </li>
      </ul>
    </details>
  );
}
