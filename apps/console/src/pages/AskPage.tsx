import { useEffect, useState, type FormEvent } from "react";
import { Link } from "react-router-dom";
import { Search } from "lucide-react";
import type { EshuApiClient } from "../api/client";
import type { RepoListItem } from "../api/repoCatalog";
import {
  askEshuQuestion,
  type AskCodeTopicEvidenceGroup,
  type AskEshuAnswer,
  type AskSemanticResult,
  type AskSourceHandle
} from "../api/askEshu";
import { uiFresh, uiTruth } from "../console/types";
import { Badge, FreshDot, Panel, TruthChip } from "../components/atoms";
import "./askPage.css";

export function AskPage({
  client,
  repositories
}: {
  readonly client?: EshuApiClient;
  readonly repositories: readonly Pick<RepoListItem, "id" | "name">[];
}): React.JSX.Element {
  const [question, setQuestion] = useState("");
  const [repoId, setRepoId] = useState(repositories[0]?.id ?? "");
  const [answer, setAnswer] = useState<AskEshuAnswer | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (repositories.length === 0) {
      if (repoId.length > 0) {
        setRepoId("");
      }
      return;
    }
    if (!repositories.some((repository) => repository.id === repoId)) {
      setRepoId(repositories[0].id);
    }
  }, [repoId, repositories]);

  async function onSubmit(event: FormEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (!client) return;
    setBusy(true);
    try {
      setAnswer(await askEshuQuestion(client, { question, repoId }));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="page ask-page">
      <div className="page-intro">
        <h2>Ask Eshu</h2>
        <p>Repository-scoped answers from bounded read routes.</p>
      </div>

      <Panel title="Question" sub="Read-only">
        <form className="ask-form" onSubmit={(event) => { void onSubmit(event); }}>
          <label>
            <span>Repository</span>
            <select
              aria-label="Repository"
              value={repoId}
              onChange={(event) => setRepoId(event.currentTarget.value)}
            >
              {repositories.map((repository) => (
                <option key={repository.id} value={repository.id}>{repository.name}</option>
              ))}
            </select>
          </label>
          <label className="ask-question-field">
            <span>Question</span>
            <textarea
              aria-label="Question"
              placeholder="How does checkout auth work?"
              value={question}
              onChange={(event) => setQuestion(event.currentTarget.value)}
            />
          </label>
          <button className="btn" disabled={busy || !client} type="submit">
            <Search aria-hidden size={16} />
            {busy ? "Asking..." : "Ask"}
          </button>
        </form>
        {repositories.length === 0 ? <p className="empty">Choose a repository before asking.</p> : null}
      </Panel>

      {answer ? <AskAnswerView answer={answer} /> : null}
    </div>
  );
}

function AskAnswerView({ answer }: { readonly answer: AskEshuAnswer }): React.JSX.Element {
  return (
    <div className="ask-answer-stack mt">
      <Panel
        title={answer.answerPacket?.promptFamily || "Answer"}
        sub={answer.status}
        action={<TruthSummary truth={answer.codeTopic.truth ?? answer.semantic.truth} />}
      >
        <div className="ask-answer-summary">
          {answer.answerPacket?.summary ? answer.answerPacket.summary : "No bounded answer from this repository."}
        </div>
        {answer.answerPacket ? (
          <div className="ask-answer-meta">
            <Badge tone={answer.answerPacket.supported ? "teal" : "warn"}>{answer.answerPacket.truthClass || "unsupported"}</Badge>
            {answer.answerPacket.partial ? <Badge tone="warn">partial</Badge> : null}
            <span className="mono">{answer.answerPacket.primaryRoute}</span>
          </div>
        ) : null}
        {answer.errors.length > 0 ? (
          <ul className="ask-errors">
            {answer.errors.map((error) => (
              <li key={`${error.source}:${error.message}`}><span className="mono">{error.source}</span> {error.message}</li>
            ))}
          </ul>
        ) : null}
      </Panel>

      <Panel
        title="Code evidence"
        sub={answer.codeTopic.route}
        action={<TruthSummary truth={answer.codeTopic.truth} />}
      >
        {answer.codeTopic.evidenceGroups.length === 0 ? (
          <p className="empty">No code-topic evidence returned.</p>
        ) : (
          <div className="ask-evidence-list">
            {answer.codeTopic.evidenceGroups.map((group, index) => (
              <EvidenceGroupCard group={group} key={`${group.entityName}:${index}`} />
            ))}
          </div>
        )}
      </Panel>

      <Panel
        title="Semantic matches"
        sub={answer.semantic.route}
        action={<TruthSummary truth={answer.semantic.truth} />}
      >
        {answer.semantic.results.length === 0 ? (
          <p className="empty">No semantic matches returned.</p>
        ) : (
          <div className="ask-semantic-list">
            {answer.semantic.results.map((result) => (
              <SemanticResultCard key={`${result.rank}:${result.title}:${result.path}`} result={result} />
            ))}
          </div>
        )}
      </Panel>
    </div>
  );
}

function TruthSummary({
  truth
}: {
  readonly truth: AskEshuAnswer["codeTopic"]["truth"];
}): React.JSX.Element | null {
  if (!truth) return null;
  return (
    <div className="ask-truth">
      <span className="mono">{truth.capability}</span>
      <TruthChip level={uiTruth(truth.level)} />
      <FreshDot state={uiFresh(truth.freshness.state)} />
    </div>
  );
}

function EvidenceGroupCard({
  group
}: {
  readonly group: AskCodeTopicEvidenceGroup;
}): React.JSX.Element {
  return (
    <article className="ask-evidence-card">
      <div>
        <strong>{group.entityName || group.sourceKind}</strong>
        <span>{group.entityType || group.language || "evidence"}</span>
      </div>
      {group.sourceHandle ? <SourceHandleLink handle={group.sourceHandle} /> : <span className="t-mut">No source handle.</span>}
    </article>
  );
}

function SourceHandleLink({ handle }: { readonly handle: AskSourceHandle }): React.JSX.Element {
  const line = handle.startLine ?? 1;
  const params = new URLSearchParams({ path: handle.relativePath, lineStart: String(line) });
  return (
    <Link className="mono" to={`/repositories/${encodeURIComponent(handle.repoId)}/source?${params.toString()}`}>
      {handle.relativePath}:{line}
    </Link>
  );
}

function SemanticResultCard({
  result
}: {
  readonly result: AskSemanticResult;
}): React.JSX.Element {
  return (
    <article className="ask-semantic-card">
      <div className="ask-semantic-head">
        <strong>{result.title}</strong>
        <span className="mono">{result.searchMethod || "search"}</span>
      </div>
      <p>{result.contextText || result.path}</p>
      <div className="ask-answer-meta">
        <Badge tone="neutral">{result.sourceKind || "document"}</Badge>
        <Badge tone="ember">{result.truthLevel || "derived"}</Badge>
        <span className="mono">{result.path || "-"}</span>
      </div>
    </article>
  );
}
