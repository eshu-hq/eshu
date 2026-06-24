// ESLint flat config for the Eshu TypeScript surfaces.
//
// Goals (per issue #3763):
//   * Catch unsafe / undisciplined TS before it lands.
//   * Keep formatting decisions out of ESLint — Prettier owns those.
//   * Extend the same rule set to the root marketing site (./src) and
//     the console package (./apps/console/src) without duplication.
//
// Audit command (local + CI): `npm run lint`.
// Acceptance contract: the `lint` job in `.github/workflows/frontend.yml`
// runs this configuration and fails on any reported violation.

import js from "@eslint/js";
import tseslint from "typescript-eslint";
import reactPlugin from "eslint-plugin-react";
import reactHooksPlugin from "eslint-plugin-react-hooks";
import importPlugin from "eslint-plugin-import";
import prettierConfig from "eslint-config-prettier";
import globals from "globals";

export default tseslint.config(
  {
    // Paths ESLint never touches. Build outputs, vendor, generated, and the
    // deliberate-violation fixture exercised by scripts/test-eslint-config.sh
    // (the fixture must remain reachable for that test but should never block
    // the regular `npm run lint` gate).
    ignores: [
      "**/node_modules/**",
      "**/dist/**",
      "site-dist/**",
      "**/coverage/**",
      "**/*.d.ts",
      "**/*.d.mts",
      "**/*.d.cts",
      "apps/console/scripts/**",
      // apps/console/prototype is a frozen, hand-rolled React reference
      // implementation kept around for design comparison. It is plain
      // JS/JSX outside any tsconfig project, so type-aware rules cannot
      // load parser services against it. Leaving it untouched is the
      // whole point of the folder; lint only the shipped surface.
      "apps/console/prototype/**",
      // apps/console/e2e is its own sub-project with its own tsconfig and
      // typecheck gate (`npm run console:e2e:typecheck`). Adding it to the
      // type-aware ESLint pass would either require dragging that tsconfig
      // in as a parallel project (slow + brittle) or duplicating the rule
      // set; deferring it to its own typecheck keeps both gates honest.
      "apps/console/e2e/**",
      // tests/fixtures/ holds the parser test corpora — sample third-party
      // projects the Go ingesters exercise against. The code there is by
      // definition representative of every shape we want to parse, not
      // code we want to lint; its globals and patterns come from many
      // languages and runtimes, so the no-undef / no-console gates cannot
      // be made happy without lying about the test data.
      "tests/fixtures/**",
      "examples/**",
      "tests/lint-fixture/**",
    ],
  },

  // Non-type-aware recommended rules applied to ALL JS/TS files (including
  // the eslint.config.js itself). These are safe to run without parser
  // services and catch the cheap-to-detect stuff (unused disable directives,
  // unreachable code, debugger statements, etc.) everywhere.
  js.configs.recommended,

  // Type-aware TypeScript rules applied ONLY to the shipped source trees
  // (./src and ./apps/console/src) — they require parser services, which
  // can only be created for files that live inside a tsconfig project.
  {
    files: ["src/**/*.ts", "src/**/*.tsx", "apps/console/src/**/*.ts", "apps/console/src/**/*.tsx"],
    extends: [...tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
      globals: {
        ...globals.browser,
        ...globals.node,
      },
    },
    plugins: {
      react: reactPlugin,
      "react-hooks": reactHooksPlugin,
      import: importPlugin,
    },
    rules: {
      // Issue #3763 acceptance: no-console.
      //
      // console.log / console.info / console.debug are treated as a hard error
      // because they leak dev-time diagnostics into shipped bundles and have
      // no place in the runtime API path. console.warn and console.error are
      // explicitly allowed because the existing error-reporting code paths in
      // apps/console/src/api/*.ts use them in catch blocks; tightening that
      // surface belongs in a follow-up, not a first-pass gate.
      "no-console": ["error", { allow: ["warn", "error"] }],

      // Issue #3763 acceptance: no-unused-vars.
      // Underscore-prefixed bindings are intentional placeholders (callback
      // args, throwaway destructures) and stay allowed.
      "@typescript-eslint/no-unused-vars": [
        "error",
        {
          argsIgnorePattern: "^_",
          varsIgnorePattern: "^_",
          caughtErrorsIgnorePattern: "^_",
        },
      ],

      // Issue #3763 acceptance: import/order.
      // Groups run external-then-internal-then-parent/sibling/index with one
      // blank line between groups and ascending alphabetical order inside a
      // group. Strict-but-boring: catches both dropped blank lines and quietly
      // out-of-order imports that hide accidental import-cycle creep.
      "import/order": [
        "error",
        {
          groups: [
            "builtin",
            "external",
            "internal",
            ["parent", "sibling", "index"],
          ],
          "newlines-between": "always",
          alphabetize: { order: "asc", caseInsensitive: true },
        },
      ],

      // Companion to import/order: type-only imports stay type-only so the
      // bundler can drop them at build time and reviewers can spot the
      // value/type distinction at a glance.
      "@typescript-eslint/consistent-type-imports": [
        "error",
        { prefer: "type-imports", fixStyle: "separate-type-imports" },
      ],

      // React 17+ JSX transform (set in tsconfig: "jsx": "react-jsx")
      // does not require importing React into scope. Disable the React 16-era
      // rules that would otherwise insist on it.
      "react/react-in-jsx-scope": "off",
      "react/jsx-uses-react": "off",

      // Hook rules — the rules-of-hooks check catches unconditional hooks in
      // conditionals / loops, which is the most common React footgun.
      "react-hooks/rules-of-hooks": "error",
      "react-hooks/exhaustive-deps": "warn",
    },
    settings: {
      react: { version: "detect" },
      "import/resolver": {
        node: { extensions: [".ts", ".tsx", ".js", ".jsx"] },
        typescript: {
          project: [
            "./tsconfig.app.json",
            "./apps/console/tsconfig.app.json",
            "./apps/console/tsconfig.node.json",
          ],
        },
      },
    },
  },

  // Tests, fixtures, and config files intentionally relax a few rules so the
  // gate does not need to be exempted per-file. The deliberate-violation
  // fixture is ignored at the top of this file; this block only softens the
  // rules for legitimate test code (asserts, mocks, intentional throws).
  {
    files: [
      "**/*.test.ts",
      "**/*.test.tsx",
      "src/test/**",
      "apps/console/src/test/**",
      "**/*.config.ts",
      "**/*.config.js",
      "vite.config.ts",
      "apps/console/vite.config.ts",
    ],
    rules: {
      "@typescript-eslint/no-explicit-any": "off",
      "@typescript-eslint/no-non-null-assertion": "off",
      // Testing-library's getByPlaceholderText / getByLabelText return
      // HTMLElement by default; tests need `as HTMLInputElement` /
      // `as HTMLSelectElement` to access `.value`. The auto-fix would
      // strip those casts and break the tests even though the typecheck
      // stays green on the strict types — they are necessary at runtime
      // for the assertions. Disable in tests so the lint gate does not
      // require per-line eslint-disable comments on every input lookup.
      "@typescript-eslint/no-unnecessary-type-assertion": "off",
    },
  },

  // scripts/ is plain ESM Node tooling — process, console, Buffer, etc. are
  // all available as Node built-ins. Marketing-review scripts additionally
  // pass Playwright page.evaluate() callbacks that run inside a browser
  // context, so they need browser globals too. Add both to the language
  // options here so the type-aware rules do not flag the obvious tooling
  // calls. Test/configs/.mjs files (e.g. console-bundle-budget.mjs) live
  // here as well.
  {
    files: ["scripts/**/*.{js,mjs,cjs,ts,mts,cts}"],
    languageOptions: {
      globals: {
        ...globals.node,
        ...globals.browser,
      },
    },
  },

  // Vite config files run in Node (so URL, process, Buffer, etc. are
  // available as built-ins) but TypeScript treats them as plain scripts.
  // Add node globals so the type-aware rules do not flag them.
  {
    files: ["vite.config.ts", "apps/console/vite.config.ts"],
    languageOptions: {
      globals: {
        ...globals.node,
      },
    },
  },

  // Opinionated rules from typescript-eslint's recommendedTypeChecked that
  // conflict with the existing codebase shape but are NOT part of the O3
  // acceptance contract. Each one is intentionally disabled here with a
  // rationale so a future maintainer can re-enable it after the matching
  // audit-and-fix PR lands.
  {
    files: ["src/**/*.ts", "src/**/*.tsx", "apps/console/src/**/*.ts", "apps/console/src/**/*.tsx"],
    rules: {
      // require-await: the codebase has many async wrappers around
      // Promise-returning functions (e.g. HTTP helpers, fetch wrappers).
      // Dropping the `async` keyword is a behaviour change in some inferred
      // return-type cases; defer the cleanup to a follow-up that audits
      // each handler. Off here so the first lint pass does not turn a
      // clean refactor into a 466-file churn.
      "@typescript-eslint/require-await": "off",
      // no-floating-promises + no-misused-promises: React handlers and
      // side-effect chains fire Promise-returning callbacks without await
      // in many places. The right fix is to wrap each one in `void ...` /
      // `.catch(...)` or to retype the handler — that is a deliberate
      // refactor PR, not a lint-gate PR. Off until each callsite is
      // audited in a follow-up.
      "@typescript-eslint/no-floating-promises": "off",
      "@typescript-eslint/no-misused-promises": "off",
      // unbound-method: vitest matchers (`expect(spy.method).toHaveBeenCalled()`)
      // and React refs routinely hand a method reference to another function.
      // The rule is correct in principle but produces hundreds of false
      // positives against the existing test suite; audit each callsite and
      // re-enable in a follow-up.
      "@typescript-eslint/unbound-method": "off",
    },
  },

  // Prettier must be the last entry so it can disable any stylistic rules
  // (e.g. indent, quotes, semi) that would otherwise fight Prettier's output.
  prettierConfig,
);
