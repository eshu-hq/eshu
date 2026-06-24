import { Link, useInRouterContext } from "react-router-dom";

import type { SuggestedQuestion } from "../api/suggestedQuestions";
import "./SuggestedQuestions.css";

export function SuggestedQuestions({
  questions
}: {
  readonly questions: readonly SuggestedQuestion[];
}): React.JSX.Element {
  const inRouter = useInRouterContext();
  if (questions.length === 0) {
    return <p className="empty suggested-questions-empty">No source-backed suggestions from this snapshot.</p>;
  }
  return (
    <div className="suggested-question-list">
      {questions.map((question) => (
        inRouter ? (
          <Link
            aria-label={question.question}
            className={`suggested-question suggested-question-${question.kind}`}
            key={question.id}
            to={question.href}
          >
            <QuestionContent question={question} />
          </Link>
        ) : (
          <a
            aria-label={question.question}
            className={`suggested-question suggested-question-${question.kind}`}
            href={question.href}
            key={question.id}
          >
            <QuestionContent question={question} />
          </a>
        )
      ))}
    </div>
  );
}

function QuestionContent({
  question
}: {
  readonly question: SuggestedQuestion;
}): React.JSX.Element {
  return (
    <>
      <span className="suggested-question-kind">{kindLabel(question.kind)}</span>
      <strong>{question.question}</strong>
      <span>{question.reason}</span>
      <code>{question.source}</code>
    </>
  );
}

function kindLabel(kind: SuggestedQuestion["kind"]): string {
  switch (kind) {
    case "code":
      return "Code";
    case "freshness":
      return "Freshness";
    case "relationship":
      return "Graph";
    case "security":
      return "Security";
  }
}
