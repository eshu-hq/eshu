// types.ts — Shared types for per-page e2e test modules.

import type { Page } from "playwright";

export interface PageTest {
  readonly path: string;
  readonly label: string;
  readonly area: string;
  assert(page: Page): Promise<void>;
}

export interface PageTestResult {
  readonly path: string;
  readonly label: string;
  readonly passed: boolean;
  readonly error?: string;
  readonly durationMs: number;
}
