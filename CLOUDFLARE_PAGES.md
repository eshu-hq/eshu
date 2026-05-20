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

## Cloudflare Pages Settings

Use these settings when creating or checking the Pages project:

| Setting | Value |
| --- | --- |
| Framework preset | Vite React |
| Build command | `npm run build` |
| Build output directory | `site-dist` |
| Production branch | Do not use `main` unless explicitly approved |

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
