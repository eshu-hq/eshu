import React, { lazy } from "react";

export function ProfileCard({ name }: { name: string }) {
  return <section>{formatTitle(name)}</section>;
}

export default function Page() {
  return <ProfileCard name="Ada" />;
}

function formatTitle(name: string): string {
  return name.toUpperCase();
}

function UnusedPanel() {
  return <aside>unused</aside>;
}

function useDynamicHook(name: string) {
  const hooks: Record<string, () => string> = {
    selected: () => name,
  };
  return hooks[name]?.();
}

export const LazyWidget = lazy(() => import("./widget"));

void useDynamicHook;
