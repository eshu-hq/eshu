/* global console, process */

import { readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const i18nDir = path.dirname(fileURLToPath(import.meta.url));
const messagesPath = path.join(i18nDir, "messages.ts");
const localesPath = path.join(i18nDir, "locales.ts");
const expectedSupportedLocales = ["en", "zh", "ja", "de", "es"];

function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const entryPath = path.join(directory, entry.name);
    if (entry.isDirectory()) return walk(entryPath);
    if (!entry.isFile()) return [];
    if (!/\.(ts|tsx)$/.test(entry.name)) return [];
    if (/\.(test|spec)\.tsx?$/.test(entry.name)) return [];
    return [entryPath];
  });
}

function readExistingFile(filePath) {
  statSync(filePath);
  return readFileSync(filePath, "utf8");
}

const defaultCatalog = readExistingFile(messagesPath);
const localeCatalog = readExistingFile(localesPath);
const defaultMessageIds = new Set(
  [...defaultCatalog.matchAll(/"(app\.[^"]+)"\s*:/g)].map((match) => match[1]),
);
const translatedMessageIds = extractStringArray(localeCatalog, "translatedMessageIds");
const supportedLocales = extractStringArray(localeCatalog, "supportedLocales");
const registryLocales = extractLocaleRegistry(localeCatalog);
const referencedMessageIds = new Set();

for (const filePath of walk(i18nDir)) {
  if (filePath === messagesPath) continue;
  if (filePath.endsWith("check-catalog.mjs")) continue;
  const source = readExistingFile(filePath);
  for (const match of source.matchAll(/\b(?:id|messageId):\s*"(app\.[^"]+)"/g)) {
    referencedMessageIds.add(match[1]);
  }
  for (const match of source.matchAll(/\bdescriptor\("(app\.[^"]+)"\)/g)) {
    referencedMessageIds.add(match[1]);
  }
}

const missing = [...referencedMessageIds].filter((id) => !defaultMessageIds.has(id)).sort();
const catalogErrors = validateLocaleCatalogs({
  defaultMessageIds,
  localeCatalog,
  registryLocales,
  supportedLocales,
  translatedMessageIds,
});

if (missing.length > 0 || catalogErrors.length > 0) {
  for (const error of catalogErrors) console.error(error);
  console.error(`Missing ${missing.length} default i18n message(s):`);
  for (const id of missing) console.error(`- ${id}`);
  process.exit(1);
}

console.log(
  `Validated ${referencedMessageIds.size} i18n message references and ${translatedMessageIds.length} translated keys across ${supportedLocales.length} locale catalogs.`,
);

function extractStringArray(source, exportName) {
  const match = source.match(
    new RegExp(`export const ${exportName}\\s*=\\s*\\[([\\s\\S]*?)\\]\\s*as const`),
  );
  if (match === null) {
    console.error(`Missing exported string array ${exportName}.`);
    process.exit(1);
  }
  return [...match[1].matchAll(/"([^"]+)"/g)].map((item) => item[1]);
}

function extractCatalogIds(source, locale, translatedIds) {
  if (locale === "en") return new Set(translatedIds);
  const match = source.match(
    new RegExp(`const ${locale}Catalog\\s*=\\s*\\{([\\s\\S]*?)\\}\\s*satisfies TranslationCatalog`),
  );
  if (match === null) return null;
  return new Set([...match[1].matchAll(/"(app\.[^"]+)"\s*:/g)].map((item) => item[1]));
}

function extractLocaleRegistry(source) {
  const match = source.match(/export const localeCatalogs:[\s\S]*?=\s*\{([\s\S]*?)\};/);
  if (match === null) {
    console.error("Missing localeCatalogs registry.");
    process.exit(1);
  }
  return [...match[1].matchAll(/^\s*([a-z]{2}):/gm)].map((item) => item[1]);
}

function validateLocaleCatalogs({
  defaultMessageIds,
  localeCatalog,
  registryLocales,
  supportedLocales,
  translatedMessageIds,
}) {
  const errors = [];
  const translatedSet = new Set(translatedMessageIds);

  if (translatedMessageIds.length !== 20) {
    errors.push(`Expected 20 translated i18n keys, found ${translatedMessageIds.length}.`);
  }
  if (translatedSet.size !== translatedMessageIds.length) {
    errors.push("Translated i18n key list contains duplicate IDs.");
  }
  const missingDefaultIds = translatedMessageIds
    .filter((messageId) => !defaultMessageIds.has(messageId))
    .sort();
  if (missingDefaultIds.length > 0) {
    errors.push(`Translated keys missing from default catalog: ${missingDefaultIds.join(", ")}.`);
  }

  const missingLocales = expectedSupportedLocales
    .filter((locale) => !supportedLocales.includes(locale))
    .sort();
  const extraLocales = supportedLocales
    .filter((locale) => !expectedSupportedLocales.includes(locale))
    .sort();
  if (missingLocales.length > 0) {
    errors.push(`Missing supported locale(s): ${missingLocales.join(", ")}.`);
  }
  if (extraLocales.length > 0) {
    errors.push(`Unexpected supported locale(s): ${extraLocales.join(", ")}.`);
  }

  const missingRegistryLocales = supportedLocales
    .filter((locale) => !registryLocales.includes(locale))
    .sort();
  const extraRegistryLocales = registryLocales
    .filter((locale) => !supportedLocales.includes(locale))
    .sort();
  if (missingRegistryLocales.length > 0) {
    errors.push(`Locale registry missing catalog(s): ${missingRegistryLocales.join(", ")}.`);
  }
  if (extraRegistryLocales.length > 0) {
    errors.push(`Locale registry has unexpected catalog(s): ${extraRegistryLocales.join(", ")}.`);
  }

  for (const locale of supportedLocales) {
    const catalogIds = extractCatalogIds(localeCatalog, locale, translatedMessageIds);
    if (catalogIds === null) {
      errors.push(`Missing ${locale} locale catalog.`);
      continue;
    }
    const missingIds = translatedMessageIds
      .filter((messageId) => !catalogIds.has(messageId))
      .sort();
    const extraIds = [...catalogIds].filter((messageId) => !translatedSet.has(messageId)).sort();
    if (missingIds.length > 0) {
      errors.push(`${locale} locale catalog missing key(s): ${missingIds.join(", ")}.`);
    }
    if (extraIds.length > 0) {
      errors.push(`${locale} locale catalog has extra key(s): ${extraIds.join(", ")}.`);
    }
  }

  return errors;
}
