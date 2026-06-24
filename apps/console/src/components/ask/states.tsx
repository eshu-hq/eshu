// states.tsx — empty/error states for the Ask Eshu surface.
//
// Each state is console-styled and explains the situation in plain language
// rather than dumping a raw error: disabled-by-config, scoped-token (403),
// demo-mode (no live backend), and generic request failure.
import { Sparkles, TriangleAlert, Zap } from "lucide-react";

import type { AskError } from "../../api/askEshu";
import { Panel } from "../atoms";

/** Friendly empty state when Ask Eshu is turned off on the deployment (503/disabled). */
export function DisabledState({ reason }: { readonly reason: string }): React.JSX.Element {
  return (
    <Panel className="ask-state-panel">
      <div className="ask-state">
        <span className="ask-state-glyph">
          <Sparkles aria-hidden />
        </span>
        <h3>Ask Eshu is turned off</h3>
        <p>{reason || "This deployment doesn't have an answer provider configured, so natural-language Q&A is disabled."}</p>
        <div className="enable-steps">
          <div className="enable-step">
            <span className="enable-n">1</span>
            <div>
              <strong>Enable the feature</strong>
              <span className="mono">ESHU_ASK_ENABLED=1</span>
            </div>
          </div>
          <div className="enable-step">
            <span className="enable-n">2</span>
            <div>
              <strong>Configure an answer provider</strong>
              <span>Set a provider profile (model + key) so Eshu can narrate over the graph.</span>
            </div>
          </div>
          <div className="enable-step">
            <span className="enable-n">3</span>
            <div>
              <strong>Use a shared / admin token</strong>
              <span>Scoped tokens can browse the graph but can&apos;t ask.</span>
            </div>
          </div>
        </div>
        <p className="t-mut ask-state-foot">
          Everything else in the console keeps working — Ask Eshu layers on top of the same read-only graph.
        </p>
      </div>
    </Panel>
  );
}

/** Empty state in demo mode, where there is no live backend to run the agent loop. */
export function DemoState(): React.JSX.Element {
  return (
    <Panel className="ask-state-panel">
      <div className="ask-state">
        <span className="ask-state-glyph">
          <Sparkles aria-hidden />
        </span>
        <h3>Ask Eshu needs a live connection</h3>
        <p>
          Ask Eshu runs an agentic loop over your live code-to-cloud graph. The prospect demo ships fixtures only, so
          there is no engine to answer against here.
        </p>
        <p className="t-mut ask-state-foot">
          Connect to a live Eshu API with a shared or admin token from the data-source menu to ask questions.
        </p>
      </div>
    </Panel>
  );
}

/** Terminal request error (forbidden / unavailable / bad request / network). */
export function AskErrorView({
  error,
  onRetry
}: {
  readonly error: AskError;
  readonly onRetry?: () => void;
}): React.JSX.Element {
  const view = ERROR_VIEWS[error.state];
  const body = view.body(error.reason);
  return (
    <Panel className="ask-state-panel">
      <div className="ask-state">
        <span className="ask-state-glyph warn">{view.glyph}</span>
        <h3>{view.title}</h3>
        <p>{body}</p>
        {onRetry && error.state !== "forbidden" ? (
          <button className="btn-ghost active" onClick={onRetry} type="button">
            Try again
          </button>
        ) : null}
      </div>
    </Panel>
  );
}

interface ErrorView {
  readonly glyph: React.JSX.Element;
  readonly title: string;
  readonly body: (reason: string) => string;
}

const ERROR_VIEWS: Record<AskError["state"], ErrorView> = {
  forbidden: {
    glyph: <Zap aria-hidden />,
    title: "This token can't ask",
    body: () =>
      "Ask Eshu requires a shared or admin token. Your current token is scoped, so the server returned 403. Connect with a broader token from the data-source menu to ask questions."
  },
  unavailable: {
    glyph: <Zap aria-hidden />,
    title: "Ask is unavailable",
    body: (reason) => reason || "The Ask service is off on this deployment."
  },
  bad_request: {
    glyph: <TriangleAlert aria-hidden />,
    title: "Empty question",
    body: () => "Type a question before submitting."
  },
  error: {
    glyph: <Zap aria-hidden />,
    title: "Couldn't reach Ask Eshu",
    body: (reason) => `${reason ? `${reason}. ` : ""}Check the connection and try again.`
  }
};
