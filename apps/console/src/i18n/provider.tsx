import { Fragment, createContext, useContext, type ReactNode } from "react";

import { defaultMessages, type MessageId } from "./messages";

export type ConsoleMessageDescriptor = {
  readonly id: MessageId;
  readonly defaultMessage?: string;
};

export type ConsoleIntl = {
  readonly formatMessage: (
    descriptor: ConsoleMessageDescriptor,
    values?: Readonly<Record<string, string | number>>,
  ) => string;
};

type RichMessageValue = string | number | ReactNode;

const ConsoleI18nContext = createContext<ConsoleIntl | null>(null);

export function createConsoleIntl(
  messages: Readonly<Record<MessageId, string>> = defaultMessages,
): ConsoleIntl {
  return {
    formatMessage: (descriptor, values) =>
      interpolateString(messages[descriptor.id] ?? descriptor.defaultMessage ?? descriptor.id, values),
  };
}

const defaultIntl = createConsoleIntl();

export function ConsoleI18nProvider({
  children,
}: {
  readonly children: ReactNode;
}): React.JSX.Element {
  return <ConsoleI18nContext.Provider value={defaultIntl}>{children}</ConsoleI18nContext.Provider>;
}

export function useConsoleIntl(): ConsoleIntl {
  const intl = useContext(ConsoleI18nContext);
  if (intl === null) throw new Error("ConsoleI18nProvider is missing");
  return intl;
}

export function FormattedMessage({
  id,
  defaultMessage,
  values,
}: ConsoleMessageDescriptor & {
  readonly values?: Readonly<Record<string, RichMessageValue>>;
}): React.JSX.Element {
  return <>{interpolateRich(defaultMessages[id] ?? defaultMessage ?? id, values)}</>;
}

function interpolateString(
  template: string,
  values: Readonly<Record<string, string | number>> = {},
): string {
  return template.replace(/\{([A-Za-z0-9_]+)\}/g, (match, key: string) => {
    const value = values[key];
    return value === undefined ? match : String(value);
  });
}

function interpolateRich(
  template: string,
  values: Readonly<Record<string, RichMessageValue>> = {},
): ReactNode {
  const parts: ReactNode[] = [];
  let cursor = 0;
  for (const match of template.matchAll(/\{([A-Za-z0-9_]+)\}/g)) {
    const token = match[0];
    const key = match[1];
    const index = match.index ?? 0;
    if (index > cursor) parts.push(template.slice(cursor, index));
    parts.push(values[key] ?? token);
    cursor = index + token.length;
  }
  if (cursor < template.length) parts.push(template.slice(cursor));
  return parts.map((part, index) => <Fragment key={index}>{part}</Fragment>);
}
