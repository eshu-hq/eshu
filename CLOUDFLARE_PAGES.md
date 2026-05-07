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

## Cloudflare MCP Use

The authorized Cloudflare MCP is for read-only verification on this branch:

- list Pages projects
- inspect the target Pages project configuration
- inspect recent deployments
- inspect deployment status and logs
- inspect configured custom domains

Do not create, update, delete, retry, roll back, purge cache, or bind domains
without separate approval.
