// AskInput.tsx — the question box, format selector and submit/cancel control.
import { Send, Square, TriangleAlert } from "lucide-react";
import { useRef } from "react";
import type { KeyboardEvent } from "react";

import { cx } from "./cx";
import type { AskFormat } from "../../api/askEshu";

/** Available answer formats shown in the selector. */
export const FORMAT_OPTIONS: ReadonlyArray<{ readonly id: AskFormat; readonly label: string }> = [
  { id: "auto", label: "Auto" },
  { id: "markdown", label: "Markdown" },
  { id: "mermaid", label: "Diagram" },
  { id: "json", label: "JSON" },
  { id: "yaml", label: "YAML" },
  { id: "csv", label: "CSV" }
];

/** The multi-line ask box with format selector and submit/cancel button. */
export function AskInput({
  question,
  onQuestionChange,
  format,
  onFormatChange,
  streaming,
  onSubmit,
  onCancel,
  invalid
}: {
  readonly question: string;
  readonly onQuestionChange: (value: string) => void;
  readonly format: AskFormat;
  readonly onFormatChange: (value: AskFormat) => void;
  readonly streaming: boolean;
  readonly onSubmit: () => void;
  readonly onCancel: () => void;
  readonly invalid: boolean;
}): React.JSX.Element {
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  function onKeyDown(event: KeyboardEvent<HTMLTextAreaElement>): void {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
      event.preventDefault();
      onSubmit();
    }
  }

  return (
    <div className={cx("ask-form", invalid && "is-invalid")}>
      <textarea
        aria-describedby={invalid ? "ask-invalid-msg" : undefined}
        aria-invalid={invalid}
        aria-label="Ask Eshu a question"
        className="ask-textarea"
        disabled={streaming}
        onChange={(event) => onQuestionChange(event.currentTarget.value)}
        onKeyDown={onKeyDown}
        placeholder="Ask about your stack — code, dependencies, infra, supply chain or runtime…"
        ref={textareaRef}
        rows={3}
        value={question}
      />
      <div className="ask-toolbar">
        <div aria-label="Answer format" className="ask-fmt" role="group">
          <span className="ask-fmt-label">Format</span>
          <div className="seg sm">
            {FORMAT_OPTIONS.map((option) => (
              <button
                className={format === option.id ? "active" : ""}
                disabled={streaming}
                key={option.id}
                onClick={() => onFormatChange(option.id)}
                type="button"
              >
                {option.label}
              </button>
            ))}
          </div>
        </div>
        <div className="ask-submit-row">
          <span aria-hidden className="ask-hint mono">
            ⌘↵
          </span>
          {streaming ? (
            <button className="ask-submit cancel" onClick={onCancel} type="button">
              <Square aria-hidden size={15} /> Cancel
            </button>
          ) : (
            <button className="ask-submit" onClick={onSubmit} type="button">
              <Send aria-hidden size={15} /> Ask Eshu
            </button>
          )}
        </div>
      </div>
      {invalid ? (
        <div className="ask-invalid" id="ask-invalid-msg" role="alert">
          <TriangleAlert aria-hidden size={13} /> Type a question first.
        </div>
      ) : null}
    </div>
  );
}
