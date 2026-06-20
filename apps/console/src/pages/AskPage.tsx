// AskPage.tsx — the Ask Eshu console surface.
//
// Fronts POST /api/v0/ask: the user asks in plain language, the backend runs
// the agentic loop over the code-to-cloud graph, and the page renders an
// evidence-backed answer. Streaming (SSE) is the default so reasoning steps
// appear live; it falls back to a synchronous request and supports cancel. The
// page leads with the truth label and evidence, stays useful when narration is
// off (evidence-only), and presents the disabled/scoped/demo states cleanly.
import { useEffect, useRef, useState } from "react";
import { Sparkles } from "lucide-react";
import type { SourceState } from "../components/SourceControls";
import {
  askEshu,
  askNarrationStatus,
  type AskAnswer,
  type AskError,
  type AskFormat,
  type AskNarrationProbe,
  type AskTraceStep
} from "../api/askEshu";
import { AskInput } from "../components/ask/AskInput";
import { ReasoningTrace } from "../components/ask/ReasoningTrace";
import { AnswerView } from "../components/ask/AnswerView";
import { AskErrorView, DemoState, DisabledState } from "../components/ask/states";
import "./askPage.css";

type Phase = "idle" | "streaming" | "done" | "error";

const FORMAT_STORAGE_KEY = "eshu.ask.format";

// Generic example prompts. They intentionally use placeholder service names so
// no customer or workspace identity is baked into the console.
const SUGGESTIONS: readonly string[] = [
  "Which services import the shared client library, and what version ranges are pinned?",
  "What's the most severe vulnerability reaching production right now?",
  "Show dead code I can safely delete",
  "Draw the dependency path for the checkout service"
];

/** The Ask Eshu page. Rendered only when the source is connected. */
export function AskPage({ source }: { readonly source: SourceState }): React.JSX.Element {
  if (source.mode === "demo") {
    return (
      <div className="page ask-page">
        <AskIntro />
        <DemoState />
      </div>
    );
  }
  return <AskLive source={source} />;
}

function AskLive({ source }: { readonly source: SourceState }): React.JSX.Element {
  const [question, setQuestion] = useState("");
  const [format, setFormat] = useState<AskFormat>(loadFormat);
  const [phase, setPhase] = useState<Phase>("idle");
  const [traces, setTraces] = useState<readonly AskTraceStep[]>([]);
  const [answer, setAnswer] = useState<AskAnswer | null>(null);
  const [error, setError] = useState<AskError | null>(null);
  const [invalid, setInvalid] = useState(false);
  const [probe, setProbe] = useState<AskNarrationProbe>({ state: "available", reason: "" });
  const abortRef = useRef<AbortController | null>(null);
  const headingRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    try {
      localStorage.setItem(FORMAT_STORAGE_KEY, format);
    } catch {
      // Storage can be unavailable (private mode); the selection is still applied.
    }
  }, [format]);

  // Capability probe decides disabled vs evidence-only presentation.
  useEffect(() => {
    let alive = true;
    void askNarrationStatus({ baseUrl: source.base, apiKey: source.key }).then((result) => {
      if (alive) {
        setProbe(result);
      }
    });
    return () => {
      alive = false;
    };
  }, [source.base, source.key]);

  // Abort any in-flight stream on unmount.
  useEffect(
    () => () => {
      abortRef.current?.abort();
    },
    []
  );

  // Move focus to the answer heading when a run completes.
  useEffect(() => {
    if (phase === "done" && answer) {
      headingRef.current?.focus();
    }
  }, [phase, answer]);

  function submit(override?: string): void {
    const next = (typeof override === "string" ? override : question).trim();
    if (next.length === 0) {
      setInvalid(true);
      setTimeout(() => setInvalid(false), 1600);
      return;
    }
    setInvalid(false);
    if (typeof override === "string") {
      setQuestion(override);
    }
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    // Ignore callbacks from a superseded run: starting a new question (or
    // unmounting) aborts the previous run, whose late onAbort/onError must not
    // clobber the current run's state.
    const current = (): boolean => abortRef.current === controller;
    setTraces([]);
    setAnswer(null);
    setError(null);
    setPhase("streaming");
    askEshu({
      connection: { baseUrl: source.base, apiKey: source.key },
      question: next,
      format,
      stream: true,
      signal: controller.signal,
      onTrace: (step) => {
        if (current()) {
          setTraces((prev) => [...prev, step]);
        }
      },
      onAnswer: (value) => {
        if (current()) {
          setAnswer(value);
        }
      },
      onError: (value) => {
        if (current()) {
          setError(value);
          setPhase("error");
        }
      },
      onDone: () => {
        if (current()) {
          setPhase((prev) => (prev === "error" ? "error" : "done"));
        }
      },
      onAbort: () => {
        if (current()) {
          setPhase("idle");
        }
      }
    });
  }

  function cancel(): void {
    abortRef.current?.abort();
    // Abandon the cancelled run entirely so a stale partial trace or answer is
    // never left on screen after the user explicitly stops.
    setTraces([]);
    setAnswer(null);
    setError(null);
    setPhase("idle");
  }

  if (probe.state === "disabled") {
    return (
      <div className="page ask-page">
        <AskIntro />
        <DisabledState reason={probe.reason} />
      </div>
    );
  }

  const streaming = phase === "streaming";
  return (
    <div className="page ask-page">
      <AskIntro />

      {probe.state === "unavailable" ? (
        <div className="prov-banner ask-evidence-banner">
          <Sparkles aria-hidden size={14} /> Narration is off on this deployment — answers are{" "}
          <strong>evidence-only</strong> right now. You&apos;ll get the reasoning trace, artifacts and evidence, but no
          written prose.
        </div>
      ) : null}

      <AskInput
        format={format}
        invalid={invalid}
        onCancel={cancel}
        onFormatChange={setFormat}
        onQuestionChange={setQuestion}
        onSubmit={() => submit()}
        question={question}
        streaming={streaming}
      />

      {phase === "idle" && !answer ? (
        <div className="ask-suggest">
          <span className="ask-suggest-label">Try</span>
          {SUGGESTIONS.map((suggestion) => (
            <button className="suggest-chip" key={suggestion} onClick={() => submit(suggestion)} type="button">
              <Sparkles aria-hidden size={13} /> {suggestion}
            </button>
          ))}
        </div>
      ) : null}

      {traces.length > 0 || streaming ? (
        <div className="mt">
          <ReasoningTrace steps={traces} streaming={streaming} />
        </div>
      ) : null}

      {phase === "error" && error ? (
        <div className="mt">
          <AskErrorView error={error} onRetry={() => submit()} />
        </div>
      ) : null}

      {answer ? (
        <div className="mt">
          <AnswerView answer={answer} headingRef={headingRef} />
        </div>
      ) : null}
    </div>
  );
}

function AskIntro(): React.JSX.Element {
  return (
    <div className="page-intro">
      <h2>Ask Eshu</h2>
      <p>
        Ask a question about your stack in plain language. Eshu runs an agentic loop over the code-to-cloud graph and
        returns an <strong>evidence-backed</strong> answer — prose, a diagram, or an exported artifact. Every answer
        leads with a confidence label and the steps it took.
      </p>
    </div>
  );
}

function loadFormat(): AskFormat {
  try {
    const stored = localStorage.getItem(FORMAT_STORAGE_KEY);
    if (stored && isFormat(stored)) {
      return stored;
    }
  } catch {
    // Ignore storage access failures; default to auto.
  }
  return "auto";
}

function isFormat(value: string): value is AskFormat {
  return ["auto", "markdown", "mermaid", "json", "yaml", "csv"].includes(value);
}
