type EventHandler = (payload: string) => string;

export function dispatchRegisteredEvent(payload: string): string {
  return payload.toUpperCase();
}

export const staticEventRegistry: Record<string, EventHandler> = {
  account_created: dispatchRegisteredEvent,
};
