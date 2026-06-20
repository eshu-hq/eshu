// CodeBlock.tsx — light syntax highlighting for JSON/YAML/CSV answer artifacts.
//
// A bounded, regex-based highlighter keyed by artifact format. It never
// evaluates the content; it only wraps recognized tokens (keys, strings,
// numbers, booleans) in styled spans so exported artifacts are legible without
// pulling a full highlighter into the bundle.
import type { ReactNode } from "react";

const TOKEN =
  /("(?:[^"\\]|\\.)*"\s*:)|("(?:[^"\\]|\\.)*")|(\btrue\b|\bfalse\b|\bnull\b)|(-?\d+\.?\d*)|(^\s*-?\s*[A-Za-z_][\w.-]*(?=:))/g;

/** A read-only, line-numbered code block with token highlighting. */
export function CodeBlock({ content, format }: { readonly content: string; readonly format: string }): React.JSX.Element {
  const lines = content.split("\n");
  return (
    <pre className="cb-pre">
      <code>
        {lines.map((line, index) => (
          <div className="cb-line" key={index}>
            <span className="cb-ln">{index + 1}</span>
            <span className="cb-txt">{highlight(line, format)}</span>
          </div>
        ))}
      </code>
    </pre>
  );
}

function highlight(line: string, format: string): ReactNode {
  if (format === "csv") {
    return highlightCsv(line);
  }
  const out: ReactNode[] = [];
  let key = 0;
  let last = 0;
  let match: RegExpExecArray | null;
  TOKEN.lastIndex = 0;
  while ((match = TOKEN.exec(line)) !== null) {
    if (match.index > last) {
      out.push(line.slice(last, match.index));
    }
    if (match[1]) {
      out.push(
        <span className="cb-key" key={key++}>
          {match[1]}
        </span>
      );
    } else if (match[2]) {
      out.push(
        <span className="cb-str" key={key++}>
          {match[2]}
        </span>
      );
    } else if (match[3]) {
      out.push(
        <span className="cb-bool" key={key++}>
          {match[3]}
        </span>
      );
    } else if (match[4]) {
      out.push(
        <span className="cb-num" key={key++}>
          {match[4]}
        </span>
      );
    } else if (match[5]) {
      out.push(
        <span className="cb-key" key={key++}>
          {match[5]}
        </span>
      );
    }
    last = match.index + match[0].length;
    if (match[0].length === 0) {
      TOKEN.lastIndex++;
    }
  }
  if (last < line.length) {
    out.push(line.slice(last));
  }
  return out.length > 0 ? out : line;
}

function highlightCsv(line: string): ReactNode {
  const parts = line.split(",");
  return parts.map((part, index) => (
    <span key={index}>
      {index > 0 ? <span className="cb-punct">,</span> : null}
      <span className={index === 0 ? "cb-key" : "cb-str"}>{part}</span>
    </span>
  ));
}
