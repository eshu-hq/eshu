// noNativeConfirm.test.ts
// Guards the #5187 migration: no console source may gate a mutation on the
// native confirm() dialog. Native confirm() blocks the renderer main thread,
// which freezes the tab under headless/CDP automation that does not
// auto-dismiss the modal, and it cannot be styled, focus-managed, or driven by
// component tests. Panels use the in-app useConfirm() hook instead.
//
// The ban covers the qualified forms only (globalThis.confirm / window.confirm).
// A bare `confirm(...)` is how the useConfirm hook's own destructured helper is
// invoked throughout the panels, so banning that token would fire on correct
// code — and a guard that flags correct code gets deleted rather than fixed.
import { readdirSync, readFileSync, statSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { describe, expect, it } from "vitest";

// Anchor on this file's own directory, never process.cwd(). Vitest runs with
// cwd at the repo root, where a sibling `src/` (the marketing site) also
// exists — resolving through cwd silently scanned that tree instead, so the
// guard passed while a native confirm() sat in the console untouched.
const SELF = fileURLToPath(import.meta.url);
const CONSOLE_SRC = dirname(SELF);

// NATIVE_CONFIRM matches an explicitly qualified native confirm() call.
const NATIVE_CONFIRM = /\b(?:globalThis|window|self)\s*\??\s*\.\s*confirm\s*\??\.?\s*\(/;

// BARE_CONFIRM matches an unqualified confirm(...) call. In a file that does
// not pull in useConfirm, that identifier resolves to the same blocking global
// — and unlike the two-argument form, a one-argument bare call still
// typechecks, so the compiler will not catch it.
const BARE_CONFIRM = /(?<![.\w])confirm\b\s*\(/;
const IMPORTS_USE_CONFIRM = /\buseConfirm\b/;

// This file-wide check cannot tell which binding a bare call resolves to: one
// component can take the hook while a second in the same file calls the
// global, and the presence test still reads clean. Real scope analysis covers
// that case — the `no-restricted-globals` rule for `confirm` in
// eslint.config.js resolves each reference against its own scope, so a
// destructured `confirm` from useConfirm() passes while a genuine global
// reference is an error. The two are complementary: ESLint sees bindings,
// this scan sees the qualified globalThis/window member expressions ESLint's
// global rule does not treat as global references.

// stripComments removes line and block comments so a prose mention of
// window.confirm() (useConfirm.tsx's own doc header) is not a violation.
function stripComments(source: string): string {
  return source.replace(/\/\*[\s\S]*?\*\//g, "").replace(/\/\/[^\n]*/g, "");
}

function sourceFiles(dir: string): readonly string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    const path = join(dir, entry);
    if (statSync(path).isDirectory()) {
      out.push(...sourceFiles(path));
      continue;
    }
    if (/\.tsx?$/.test(entry)) {
      out.push(path);
    }
  }
  return out;
}

describe("native confirm() ban (#5187)", () => {
  it("no console source gates a mutation on the blocking native dialog", () => {
    const offenders = sourceFiles(CONSOLE_SRC)
      // This scanner spells the banned pattern out in its own regex and
      // failure message, so it must not scan itself.
      .filter((path) => path !== SELF)
      .filter((path) => {
        const code = stripComments(readFileSync(path, "utf8"));
        if (NATIVE_CONFIRM.test(code)) return true;
        return BARE_CONFIRM.test(code) && !IMPORTS_USE_CONFIRM.test(code);
      })
      .map((path) => path.slice(CONSOLE_SRC.length + 1));

    expect(offenders).toEqual([]);
  });
});
