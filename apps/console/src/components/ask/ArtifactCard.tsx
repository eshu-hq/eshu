// ArtifactCard.tsx — renders one answer artifact by format.
//
// Markdown renders as prose; Mermaid renders as a diagram (lazy-loaded, with a
// graceful fall back to the diagram source on render failure); JSON/YAML/CSV
// render in a highlighted, line-numbered block. Every artifact offers Copy and
// Download, and any per-format `issues[]` are shown as non-blocking warnings.
import { Box, Check, Copy, Download, TriangleAlert } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { CodeBlock } from "./CodeBlock";
import { cx } from "./cx";
import { renderMarkdown } from "./markdown";
import { renderMermaid } from "./mermaid";
import type { AskArtifact } from "../../api/askEshu";

const FORMAT_LABEL: Record<string, string> = {
  auto: "Auto",
  markdown: "Markdown",
  mermaid: "Diagram",
  json: "JSON",
  yaml: "YAML",
  csv: "CSV"
};

const DOWNLOAD_EXT: Record<string, string> = {
  mermaid: "mmd",
  markdown: "md",
  json: "json",
  yaml: "yaml",
  csv: "csv"
};

type MermaidView = "diagram" | "source";

/** Render a single answer artifact with copy/download and format-aware view. */
export function ArtifactCard({ artifact }: { readonly artifact: AskArtifact }): React.JSX.Element {
  const format = artifact.format;
  const isMermaid = format === "mermaid";
  const isMarkdown = format === "markdown";
  const [copied, setCopied] = useState(false);
  const issueCount = artifact.issues.length;

  return (
    <div className="artifact-card">
      <div className="artifact-head">
        <span className="artifact-fmt">
          <Box aria-hidden size={13} /> {FORMAT_LABEL[format] ?? format}
        </span>
        {issueCount > 0 ? (
          <span className="artifact-issues" title={artifact.issues.join("\n")}>
            <TriangleAlert aria-hidden size={12} /> {issueCount} note{issueCount > 1 ? "s" : ""}
          </span>
        ) : null}
        <ArtifactActions artifact={artifact} copied={copied} setCopied={setCopied} />
      </div>
      <div className="artifact-body">
        {isMarkdown ? (
          <div className="answer-prose">{renderMarkdown(artifact.content)}</div>
        ) : isMermaid ? (
          <MermaidArtifact content={artifact.content} format={format} />
        ) : (
          <CodeBlock content={artifact.content} format={format} />
        )}
      </div>
      {issueCount > 0 ? (
        <ul className="artifact-issue-list">
          {artifact.issues.map((issue, index) => (
            <li key={index}>
              <TriangleAlert aria-hidden size={12} /> {issue}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function ArtifactActions({
  artifact,
  copied,
  setCopied
}: {
  readonly artifact: AskArtifact;
  readonly copied: boolean;
  readonly setCopied: (value: boolean) => void;
}): React.JSX.Element {
  async function copy(): Promise<void> {
    try {
      await navigator.clipboard.writeText(artifact.content);
      setCopied(true);
      setTimeout(() => setCopied(false), 1400);
    } catch {
      // Clipboard access can be denied; the Download action remains available.
    }
  }

  function download(): void {
    const ext = DOWNLOAD_EXT[artifact.format] ?? "txt";
    const blob = new Blob([artifact.content], { type: "text/plain" });
    const url = URL.createObjectURL(blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = `eshu-answer.${ext}`;
    document.body.appendChild(anchor);
    anchor.click();
    anchor.remove();
    setTimeout(() => URL.revokeObjectURL(url), 2000);
  }

  return (
    <div className="artifact-actions">
      <button className="btn-ghost sm" onClick={() => void copy()} type="button">
        {copied ? (
          <>
            <Check aria-hidden size={13} /> Copied
          </>
        ) : (
          <>
            <Copy aria-hidden size={13} /> Copy
          </>
        )}
      </button>
      <button className="btn-ghost sm" onClick={download} type="button">
        <Download aria-hidden size={13} /> Download
      </button>
    </div>
  );
}

function MermaidArtifact({ content, format }: { readonly content: string; readonly format: string }): React.JSX.Element {
  const [view, setView] = useState<MermaidView>("diagram");
  const [svg, setSvg] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);
  const idRef = useRef(`mmd-${Math.random().toString(36).slice(2)}`);
  const renderCountRef = useRef(0);

  useEffect(() => {
    if (view !== "diagram") {
      return;
    }
    let alive = true;
    setFailed(false);
    // Mermaid creates scratch DOM keyed by this id, so each render call needs a
    // fresh id; reusing one across Diagram↔Source toggles corrupts the output.
    renderCountRef.current += 1;
    const renderId = `${idRef.current}-${renderCountRef.current}`;
    renderMermaid(renderId, content)
      .then((result) => {
        if (alive) {
          setSvg(result);
        }
      })
      .catch(() => {
        if (alive) {
          setFailed(true);
          setView("source");
        }
      });
    return () => {
      alive = false;
    };
  }, [content, view]);

  return (
    <>
      <div className="seg sm artifact-view-toggle">
        <button className={view === "diagram" ? "active" : ""} onClick={() => setView("diagram")} type="button">
          Diagram
        </button>
        <button className={view === "source" ? "active" : ""} onClick={() => setView("source")} type="button">
          Source
        </button>
      </div>
      {view === "diagram" ? (
        failed ? (
          <div className="artifact-fallback">
            <TriangleAlert aria-hidden size={14} /> Diagram couldn&apos;t render — showing source.
          </div>
        ) : svg ? (
          // svg is Mermaid `securityLevel: "strict"` output, sanitized by
          // Mermaid's bundled DOMPurify before it is returned (see mermaid.ts).
          <div className={cx("mermaid-wrap")} dangerouslySetInnerHTML={{ __html: svg }} />
        ) : (
          <div className="artifact-loading">Rendering diagram…</div>
        )
      ) : (
        <CodeBlock content={content} format={format} />
      )}
    </>
  );
}
