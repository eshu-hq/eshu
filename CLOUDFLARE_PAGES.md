# Eshu Cloudflare Pages Site

This branch adds a static Vite React site for Eshu. It is meant to run from the
repo root on Cloudflare Pages.

## Local Commands

```bash
npm install
npm run dev
npm run typecheck
npm test
npm run build
```

## Browser Review Gate

Before shipping a marketing-site change, run the local browser review gate. It
is a repeatable Playwright check of the ROOT marketing site (the site that
builds to `site-dist`), kept separate from any console private-data proof.

The gate does not require Node's native TypeScript loader; it transforms the
unit-tested evaluator through Vite before running the browser checks.

```bash
npx playwright install chromium   # one-time, if Chromium is missing
npm run site:review
```

`npm run site:review` runs `scripts/marketing-review.mjs`, which:

- builds the root site (`npm run build` -> `site-dist`),
- serves the static build with `vite preview` (the same static path Cloudflare
  Pages serves — no Workers-only behavior is exercised),
- loads the site in Chromium at a desktop (1440x900) AND a mobile (390x844)
  viewport,
- verifies primary routes (anchor sections), nav links, CTAs, external links,
  and the Ask Eshu / first-run positioning do not regress,
- runs basic accessibility (image alt text, single `<h1>`) and performance
  (DOMContentLoaded budget) checks,
- captures desktop + mobile screenshots, and
- exits non-zero on any failed check.

Flags:

- `npm run site:review -- --no-build` reuses an existing `site-dist`.
- `npm run site:review -- --keep-build` keeps `site-dist` after the run.

Artifacts (screenshots plus a `marketing-review.json` summary) are written to
`site-review-artifacts/`, which is gitignored. Do not commit those binaries.

The pass/fail logic lives in `src/marketingReview.ts` and is unit-tested by
`src/marketingReview.test.ts` (run under `npm test`), so the regression contract
is verified without a browser; the script proves the rendered page honors it.

## Cloudflare Pages Settings

Use these settings when creating or checking the Pages project:

| Setting | Value |
| --- | --- |
| Framework preset | Vite React |
| Build command | `npm run build` |
| Build output directory | `site-dist` |
| Node version | `22.12.0` (pinned via `.nvmrc`) |
| Production branch | Do not use `main` unless explicitly approved |

### Node version

The build requires Node `>=20.19` or `>=22.12` because Vite 7 declares that
engine range. The repo pins `22.12.0` in `.nvmrc`, which Cloudflare Pages reads
to select the build Node version. Without this pin, a Pages project still on an
older default build image (e.g. Node 18.17.1) would run `npm run build` under an
unsupported Node and fail. If a project sets `NODE_VERSION` explicitly in the
Pages dashboard, keep it `>= 22.12`.

The Pages output directory is also declared in `wrangler.jsonc`:

```jsonc
{
  "pages_build_output_dir": "./site-dist"
}
```

## Release Gate

Cloudflare Pages is the only Cloudflare deploy surface for this repository.
Workers Builds are not a release gate for Eshu.

Cloudflare's GitHub App is shared by Workers and Pages, and connected Workers
can emit `Workers Builds: <name>` check runs for commits and pull requests. If a
`Workers Builds: eshu` check appears on this repository, treat it as a
Cloudflare-side integration issue unless a future change intentionally adds a
Worker source and release gate.

Expected release signals:

- repo-owned GitHub Actions pass
- the Cloudflare Pages deployment/check passes when Pages is enabled
- no Cloudflare Workers build is required for Eshu release readiness

To fix recurring Workers build noise, inspect the Cloudflare Workers service
named `eshu` and disconnect, disable, or narrow its Git integration so it no
longer builds this repository. Do not add a dummy Worker just to make the
external check green; that would create a deployment surface Eshu does not
currently use.

## Cloudflare MCP Use

The authorized Cloudflare MCP is for read-only verification on this branch:

- list Pages projects
- inspect the target Pages project configuration
- inspect recent deployments
- inspect deployment status and logs
- inspect configured custom domains

Do not create, update, delete, retry, roll back, purge cache, or bind domains
without separate approval.
