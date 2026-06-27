import { Fragment, createContext, useContext, useMemo, type ReactNode } from "react";

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
      interpolateString(
        messages[descriptor.id] ?? descriptor.defaultMessage ?? descriptor.id,
        values,
      ),
  };
}

export function ConsoleI18nProvider({
  children,
  messages = defaultMessages,
}: {
  readonly children: ReactNode;
  readonly messages?: Readonly<Record<MessageId, string>>;
}): React.JSX.Element {
  const intl = useMemo(() => createConsoleIntl(messages), [messages]);

  return <ConsoleI18nContext.Provider value={intl}>{children}</ConsoleI18nContext.Provider>;
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
  const intl = useConsoleIntl();
  return <>{interpolateRich(intl.formatMessage({ id, defaultMessage }), values)}</>;
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
