// markdown.tsx — a tiny, dependency-free Markdown renderer for answer prose.
//
// The Ask engine narrates in lightweight Markdown (headings, lists, tables,
// inline code/bold/links). Rather than pull a full Markdown stack into the
// console bundle, this renders the bounded subset the engine emits. Link hrefs
// are sanitized to http(s)/mailto so a narrated answer cannot smuggle a
// javascript: URL into the DOM.
import type { ReactNode } from "react";

type Block =
  | { readonly kind: "heading"; readonly level: number; readonly text: string }
  | { readonly kind: "list"; readonly items: readonly string[] }
  | { readonly kind: "table"; readonly rows: readonly string[] }
  | { readonly kind: "paragraph"; readonly text: string };

const INLINE = /(`[^`]+`)|(\*\*[^*]+\*\*)|(\[[^\]]+\]\([^)]+\))/g;
const LINK = /\[([^\]]+)\]\(([^)]+)\)/;

/** Render inline Markdown (code, bold, links) within a single text run. */
export function parseInline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let key = 0;
  let last = 0;
  let match: RegExpExecArray | null;
  INLINE.lastIndex = 0;
  while ((match = INLINE.exec(text)) !== null) {
    if (match.index > last) {
      out.push(text.slice(last, match.index));
    }
    const token = match[0];
    if (token.startsWith("`")) {
      out.push(
        <code className="md-code" key={key++}>
          {token.slice(1, -1)}
        </code>
      );
    } else if (token.startsWith("**")) {
      out.push(<strong key={key++}>{token.slice(2, -2)}</strong>);
    } else {
      const link = LINK.exec(token);
      const href = link ? safeHref(link[2]) : null;
      if (link && href) {
        out.push(
          <a className="md-link" href={href} key={key++} rel="noreferrer" target="_blank">
            {link[1]}
          </a>
        );
      } else if (link) {
        out.push(<span key={key++}>{link[1]}</span>);
      }
    }
    last = match.index + token.length;
  }
  if (last < text.length) {
    out.push(text.slice(last));
  }
  return out;
}

/** Render a bounded Markdown document to React nodes. */
export function renderMarkdown(markdown: string): ReactNode {
  if (!markdown) {
    return null;
  }
  return parseBlocks(markdown).map((block, index) => renderBlock(block, index));
}

function parseBlocks(markdown: string): Block[] {
  const lines = markdown.replace(/\r/g, "").split("\n");
  const blocks: Block[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    if (/^\s*$/.test(line)) {
      i++;
      continue;
    }
    const heading = /^(#{1,3})\s+(.*)/.exec(line);
    if (heading) {
      blocks.push({ kind: "heading", level: heading[1].length, text: heading[2] });
      i++;
      continue;
    }
    if (line.includes("|")) {
      const rows: string[] = [];
      while (i < lines.length && lines[i].includes("|")) {
        rows.push(lines[i]);
        i++;
      }
      blocks.push({ kind: "table", rows });
      continue;
    }
    if (/^\s*[-*]\s+/.test(line)) {
      const items: string[] = [];
      while (i < lines.length && /^\s*[-*]\s+/.test(lines[i])) {
        items.push(lines[i].replace(/^\s*[-*]\s+/, ""));
        i++;
      }
      blocks.push({ kind: "list", items });
      continue;
    }
    const paragraph: string[] = [];
    while (
      i < lines.length &&
      !/^\s*$/.test(lines[i]) &&
      !/^(#{1,3})\s+/.test(lines[i]) &&
      !lines[i].includes("|") &&
      !/^\s*[-*]\s+/.test(lines[i])
    ) {
      paragraph.push(lines[i]);
      i++;
    }
    blocks.push({ kind: "paragraph", text: paragraph.join(" ") });
  }
  return blocks;
}

function renderBlock(block: Block, index: number): ReactNode {
  if (block.kind === "heading") {
    const Tag = `h${Math.min(4, block.level + 1)}` as "h2" | "h3" | "h4";
    return (
      <Tag className="md-h" key={index}>
        {parseInline(block.text)}
      </Tag>
    );
  }
  if (block.kind === "list") {
    return (
      <ul className="md-ul" key={index}>
        {block.items.map((item, itemIndex) => (
          <li key={itemIndex}>{parseInline(item)}</li>
        ))}
      </ul>
    );
  }
  if (block.kind === "table") {
    return renderTable(block.rows, index);
  }
  return (
    <p className="md-p" key={index}>
      {parseInline(block.text)}
    </p>
  );
}

function renderTable(rows: readonly string[], index: number): ReactNode {
  const cells = rows.map((row) =>
    row
      .trim()
      .replace(/^\|/, "")
      .replace(/\|$/, "")
      .split("|")
      .map((cell) => cell.trim())
  );
  const body = cells.filter((row) => !row.every((cell) => /^:?-+:?$/.test(cell) || cell === ""));
  const head = body[0] ?? [];
  const data = body.slice(1);
  return (
    <div className="md-table-wrap" key={index}>
      <table className="md-table">
        <thead>
          <tr>
            {head.map((cell, cellIndex) => (
              <th key={cellIndex}>{parseInline(cell)}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.map((row, rowIndex) => (
            <tr key={rowIndex}>
              {row.map((cell, cellIndex) => (
                <td key={cellIndex}>{parseInline(cell)}</td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

// safeHref allows only protocols that cannot execute script. Relative links
// (no scheme) are permitted; anything with a disallowed scheme is rejected.
function safeHref(raw: string): string | null {
  const value = raw.trim();
  if (value.length === 0) {
    return null;
  }
  if (/^(https?:|mailto:)/i.test(value)) {
    return value;
  }
  if (/^[a-z][a-z0-9+.-]*:/i.test(value)) {
    // Has a scheme, but not an allowed one (e.g. javascript:, data:).
    return null;
  }
  return value;
}
