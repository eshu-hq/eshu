# Dynamic Open Source Product Demo Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Turn the Eshu public site from a static marketing page into an interactive open-source product demo that earns buy-in from engineers and organization leaders.

**Architecture:** Keep the site static-hostable on Cloudflare Pages with React local state and seeded demo data. The hero graph, terminal, persona explorer, scale proof, and cleanup toggle share one selected demo scenario so interactions feel connected instead of decorative.

**Tech Stack:** Vite, React, TypeScript strict mode, Vitest, Testing Library, CSS modules by section.

---

### Task 1: Demo Content Model

**Files:**
- Modify: `src/siteContent.ts`
- Test: `src/siteContent.test.ts`

**Steps:**
1. Add failing tests for graph scenarios, CLI command outputs, persona answers, scale metrics, and cleanup examples.
2. Run `npm test` and verify the new tests fail because content fields do not exist.
3. Add typed content for a default `checkout-service` trace across Code, SQL, Terraform, Kubernetes, Cloud, Runtime, and Docs.
4. Run `npm test` and verify content tests pass.

### Task 2: Interactive Product Demo

**Files:**
- Modify: `src/App.tsx`
- Modify: `src/styles.css`
- Modify: `src/contentSections.css`
- Modify: `src/responsive.css`
- Test: `src/App.test.tsx`

**Steps:**
1. Add failing UI tests for command buttons, persona tabs, selected graph node state, scale metrics, and cleanup toggle.
2. Run `npm test` and verify the App test fails for missing controls.
3. Implement local state in React for selected graph node, command, persona, and cleanup mode.
4. Render accessible buttons/tabs and update graph labels, terminal output, persona answer, and cleanup findings.
5. Run `npm test` and verify all tests pass.

### Task 3: Visual Polish And Verification

**Files:**
- Modify: `src/styles.css`
- Modify: `src/contentSections.css`
- Modify: `src/responsive.css`

**Steps:**
1. Run `npm run typecheck`.
2. Run `npm run build`.
3. Run the local dev server and inspect the site in the in-app browser.
4. Verify mobile and desktop layouts do not clip nav, graph labels, terminal output, cards, or CTAs.
5. Run the banned phrase scan with `rg`.
