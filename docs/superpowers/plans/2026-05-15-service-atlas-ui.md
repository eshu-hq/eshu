# Service Atlas UI Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the service page into a Service Atlas that shows story, coverage, graph proof, dual deployment lanes, API surface, dependencies, consumers, and evidence drilldowns from the latest MCP contracts.

**Architecture:** Keep the slice frontend-only and driven by the existing `ServiceSpotlight` adapter. Add atlas-specific layout components beside `ServiceSpotlightPanel.tsx`, using existing typed service data and investigation coverage. Keep graph rendering in `ServiceDeploymentLaneMap.tsx` and split CSS by product surface so touched files remain under 500 lines.

**Tech Stack:** React, TypeScript, Vitest, Testing Library, D3-backed SVG graph components, existing Eshu Console CSS tokens.

---

## Chunk 1: Service Atlas Structure

### Task 1: Add atlas-level behavior tests

**Files:**
- Modify: `apps/console/src/pages/ServiceSpotlightPanel.test.tsx`
- Modify: `apps/console/src/pages/ServiceSpotlightPanel.tsx`
- Add: `apps/console/src/pages/ServiceAtlasEvidence.tsx`
- Add: `apps/console/src/serviceAtlas.css`

- [x] **Step 1: Write the failing test**

Add assertions that the service page exposes:

```tsx
expect(screen.getByRole("heading", { name: "Service Atlas" })).toBeInTheDocument();
expect(screen.getByText("Partial coverage")).toBeInTheDocument();
expect(screen.getByText("26 repositories")).toBeInTheDocument();
expect(screen.getByText("6 evidence families")).toBeInTheDocument();
expect(screen.getByRole("complementary", { name: "Evidence drilldown" })).toBeInTheDocument();
```

- [x] **Step 2: Run test to verify it fails**

Run: `npm run console:test -- apps/console/src/pages/ServiceSpotlightPanel.test.tsx`

Expected: FAIL because the atlas heading, trust strip, and drilldown rail do not exist yet.

- [x] **Step 3: Implement minimal atlas layout**

Add `ServiceTrustStrip` and `ServiceEvidenceRail` helpers in `ServiceAtlasEvidence.tsx`. Reuse existing `spotlight.investigation`, `spotlight.api`, `spotlight.lanes`, and relationship counts.

- [x] **Step 4: Run focused test to verify it passes**

Run: `npm run console:test -- apps/console/src/pages/ServiceSpotlightPanel.test.tsx`

Expected: PASS.

## Chunk 2: Graph Workbench

### Task 2: Make the graph and rail feel like one drilldown system

**Files:**
- Modify: `apps/console/src/pages/ServiceSpotlightPanel.test.tsx`
- Modify: `apps/console/src/pages/ServiceSpotlightPanel.tsx`
- Add: `apps/console/src/serviceAtlas.css`

- [x] **Step 1: Write the failing test**

Assert that the main workbench has a labeled graph region, readable dual-lane labels, and relationship verbs visible in the rail.

- [x] **Step 2: Run test to verify it fails**

Run: `npm run console:test -- apps/console/src/pages/ServiceSpotlightPanel.test.tsx`

- [x] **Step 3: Implement graph workbench layout**

Wrap the deployment map and relationship summary in an atlas workbench grid. Move the selected evidence summary into a right-side rail near the graph.

- [x] **Step 4: Run focused test to verify it passes**

Run: `npm run console:test -- apps/console/src/pages/ServiceSpotlightPanel.test.tsx`

## Chunk 3: Verification

### Task 3: Run console gates and browser smoke

**Files:**
- Verify only.

- [x] **Step 1: Run focused tests**

Run: `npm run console:test -- apps/console/src/pages/ServiceSpotlightPanel.test.tsx`

- [x] **Step 2: Run full console tests**

Run: `npm run console:test`

- [x] **Step 3: Run typecheck and build**

Run: `npm run console:typecheck`

Run: `npm run console:build`

- [x] **Step 4: Browser smoke**

Run the console against the remote or local live data and capture a Playwright screenshot for the service page. Verify readable dual deployment, trust strip, graph, and evidence rail.

Result: remote live service page for `api-node-boats` showed Service Atlas, dual ECS Terraform plus Kubernetes GitOps lanes, 38 endpoints, 42 downstream relationships, Derived truth, and the Evidence drilldown rail.
