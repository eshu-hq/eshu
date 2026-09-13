import "@testing-library/jest-dom/vitest";
import { configure } from "@testing-library/react";
import { afterEach, vi } from "vitest";

// This suite runs on a shared host where other sessions' preflights and
// builds push the load average to 25-40 (observed while vitest itself uses
// several cores). Testing Library's default 1s findBy*/waitFor budget then
// expires on lazily rendered routes (AppDemoMode's /impact heading) even
// though the same run passes at idle. A 5s budget keeps every assertion and
// removes the contention-induced flake; vite.config.ts raises vitest's
// per-test timeout above it so a genuinely missing element still fails with
// Testing Library's own error and DOM dump, just later.
configure({ asyncUtilTimeout: 5000 });

class MemoryStorage implements Storage {
  private readonly values = new Map<string, string>();

  get length(): number {
    return this.values.size;
  }

  clear(): void {
    this.values.clear();
  }

  getItem(key: string): string | null {
    return this.values.get(key) ?? null;
  }

  key(index: number): string | null {
    return Array.from(this.values.keys())[index] ?? null;
  }

  removeItem(key: string): void {
    this.values.delete(key);
  }

  setItem(key: string, value: string): void {
    this.values.set(key, value);
  }
}

Object.defineProperty(window, "localStorage", {
  configurable: true,
  value: new MemoryStorage(),
});

afterEach(() => {
  vi.unstubAllGlobals();
});
