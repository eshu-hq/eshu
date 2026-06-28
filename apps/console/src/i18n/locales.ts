import { defaultMessages, type MessageId } from "./messages";

export const supportedLocales = ["en", "zh", "ja", "de", "es"] as const;

export type SupportedLocale = (typeof supportedLocales)[number];

export const translatedMessageIds = [
  "app.nav.group.overview",
  "app.nav.group.inventory",
  "app.nav.group.code",
  "app.nav.group.cloudTelemetry",
  "app.nav.group.system",
  "app.nav.item.status",
  "app.nav.item.dashboard",
  "app.nav.item.ask",
  "app.nav.item.impact",
  "app.nav.item.exposurePath",
  "app.nav.item.changedSince",
  "app.nav.item.graphExplorer",
  "app.nav.item.relationships",
  "app.shell.title",
  "app.shell.brandSubtitle",
  "app.shell.subtitle",
  "app.shell.source.live",
  "app.shell.search.button",
  "app.shell.search.input",
  "app.shell.error.apiUnavailable",
] as const satisfies readonly MessageId[];

export type TranslatedMessageId = (typeof translatedMessageIds)[number];

export type TranslationCatalog = Readonly<Record<TranslatedMessageId, string>>;

const zhCatalog = {
  "app.nav.group.overview": "概览",
  "app.nav.group.inventory": "清单",
  "app.nav.group.code": "代码",
  "app.nav.group.cloudTelemetry": "云与遥测",
  "app.nav.group.system": "系统",
  "app.nav.item.status": "状态",
  "app.nav.item.dashboard": "仪表板",
  "app.nav.item.ask": "询问 Eshu",
  "app.nav.item.impact": "影响",
  "app.nav.item.exposurePath": "暴露路径",
  "app.nav.item.changedSince": "变更时间",
  "app.nav.item.graphExplorer": "图谱浏览器",
  "app.nav.item.relationships": "关系",
  "app.shell.title": "Eshu 控制台",
  "app.shell.brandSubtitle": "上下文图谱",
  "app.shell.subtitle": "只读代码到云图谱状态与证据",
  "app.shell.source.live": "实时",
  "app.shell.search.button": "搜索",
  "app.shell.search.input": "搜索 Eshu",
  "app.shell.error.apiUnavailable": "Eshu API 在 {base}{detail} 不可用。",
} satisfies TranslationCatalog;

const jaCatalog = {
  "app.nav.group.overview": "概要",
  "app.nav.group.inventory": "インベントリ",
  "app.nav.group.code": "コード",
  "app.nav.group.cloudTelemetry": "クラウドとテレメトリ",
  "app.nav.group.system": "システム",
  "app.nav.item.status": "ステータス",
  "app.nav.item.dashboard": "ダッシュボード",
  "app.nav.item.ask": "Eshu に質問",
  "app.nav.item.impact": "影響",
  "app.nav.item.exposurePath": "露出パス",
  "app.nav.item.changedSince": "変更履歴",
  "app.nav.item.graphExplorer": "グラフエクスプローラー",
  "app.nav.item.relationships": "関係",
  "app.shell.title": "Eshu コンソール",
  "app.shell.brandSubtitle": "コンテキストグラフ",
  "app.shell.subtitle": "読み取り専用のコードからクラウドまでのグラフ状態と証拠",
  "app.shell.source.live": "ライブ",
  "app.shell.search.button": "検索",
  "app.shell.search.input": "Eshu を検索",
  "app.shell.error.apiUnavailable": "Eshu APIは {base}{detail} で利用できません。",
} satisfies TranslationCatalog;

const deCatalog = {
  "app.nav.group.overview": "Übersicht",
  "app.nav.group.inventory": "Bestand",
  "app.nav.group.code": "Code",
  "app.nav.group.cloudTelemetry": "Cloud & Telemetrie",
  "app.nav.group.system": "System",
  "app.nav.item.status": "Status",
  "app.nav.item.dashboard": "Dashboard",
  "app.nav.item.ask": "Eshu fragen",
  "app.nav.item.impact": "Auswirkungen",
  "app.nav.item.exposurePath": "Expositionspfad",
  "app.nav.item.changedSince": "Änderungen seit",
  "app.nav.item.graphExplorer": "Graph-Explorer",
  "app.nav.item.relationships": "Beziehungen",
  "app.shell.title": "Eshu-Konsole",
  "app.shell.brandSubtitle": "Kontextgraph",
  "app.shell.subtitle": "Schreibgeschützter Code-to-Cloud-Graphstatus und Evidenz",
  "app.shell.source.live": "Live",
  "app.shell.search.button": "Suchen",
  "app.shell.search.input": "Eshu durchsuchen",
  "app.shell.error.apiUnavailable": "Eshu-API unter {base}{detail} nicht verfügbar.",
} satisfies TranslationCatalog;

const esCatalog = {
  "app.nav.group.overview": "Resumen",
  "app.nav.group.inventory": "Inventario",
  "app.nav.group.code": "Código",
  "app.nav.group.cloudTelemetry": "Nube y telemetría",
  "app.nav.group.system": "Sistema",
  "app.nav.item.status": "Estado",
  "app.nav.item.dashboard": "Panel",
  "app.nav.item.ask": "Preguntar a Eshu",
  "app.nav.item.impact": "Impacto",
  "app.nav.item.exposurePath": "Ruta de exposición",
  "app.nav.item.changedSince": "Cambios desde",
  "app.nav.item.graphExplorer": "Explorador de grafos",
  "app.nav.item.relationships": "Relaciones",
  "app.shell.title": "Consola de Eshu",
  "app.shell.brandSubtitle": "Grafo de contexto",
  "app.shell.subtitle": "Estado y evidencia del grafo de código a nube en modo lectura",
  "app.shell.source.live": "En vivo",
  "app.shell.search.button": "Buscar",
  "app.shell.search.input": "Buscar en Eshu",
  "app.shell.error.apiUnavailable": "La API de Eshu no está disponible en {base}{detail}.",
} satisfies TranslationCatalog;

export const localeCatalogs: Readonly<Record<SupportedLocale, TranslationCatalog>> = {
  en: selectDefaultMessages(translatedMessageIds),
  zh: zhCatalog,
  ja: jaCatalog,
  de: deCatalog,
  es: esCatalog,
};

export function createLocaleMessages(locale: SupportedLocale): Readonly<Record<MessageId, string>> {
  return {
    ...defaultMessages,
    ...localeCatalogs[locale],
  };
}

function selectDefaultMessages(messageIds: readonly TranslatedMessageId[]): TranslationCatalog {
  return Object.fromEntries(
    messageIds.map((messageId) => [messageId, defaultMessages[messageId]] as const),
  ) as TranslationCatalog;
}
