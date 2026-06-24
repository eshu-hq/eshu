// Test-only ESLint config used by scripts/test-eslint-config.sh.
//
// This file lives inside tests/lint-fixture so it sits next to the fixture
// files and can resolve the @eslint/js, typescript-eslint, and related
// packages from the project's node_modules tree (ESM resolution starts
// from the config file's directory).
//
// The main eslint.config.js at the repo root ignores everything under
// tests/lint-fixture/**, so this file is NEVER scanned by `npm run lint`.
// scripts/test-eslint-config.sh invokes eslint explicitly with
// `--config tests/lint-fixture/eslint.test.config.mjs` to drive the
// negative (bad fixture), positive (clean fixture), and import-order
// (order-bad fixture) cases.

import js from "@eslint/js";
import tseslint from "typescript-eslint";
import importPlugin from "eslint-plugin-import";

export default tseslint.config(
  { ignores: ["**/node_modules/**"] },
  js.configs.recommended,
  ...tseslint.configs.recommendedTypeChecked,
  {
    files: ["**/*.ts"],
    plugins: {
      import: importPlugin,
    },
    languageOptions: {
      parserOptions: {
        project: ["./tsconfig.test.json"],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // Mirrors the rule set the production eslint.config.js applies to the
      // real source tree. We re-state the rules here (rather than importing
      // the production config) so this test stays self-contained and does
      // not break the moment a future edit adds an extra plugin that the
      // scratch tsconfig cannot satisfy.
      "no-console": ["error", { allow: ["warn", "error"] }],
      "@typescript-eslint/no-unused-vars": [
        "error",
        { argsIgnorePattern: "^_", varsIgnorePattern: "^_", caughtErrorsIgnorePattern: "^_" },
      ],
      "@typescript-eslint/consistent-type-imports": [
        "error",
        { prefer: "type-imports", fixStyle: "separate-type-imports" },
      ],
      "import/order": [
        "error",
        {
          groups: ["builtin", "external", "internal", ["parent", "sibling", "index"]],
          "newlines-between": "always",
          alphabetize: { order: "asc", caseInsensitive: true },
        },
      ],
    },
  },
);
