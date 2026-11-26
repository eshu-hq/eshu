// contractShape.testSupport.ts
// A strict, dependency-free validator for the OpenAPI subset the Go spec uses
// (type / properties / required / items / enum / nullable / additionalProperties
// / oneOf / anyOf / allOf). It reports every value that a real client would
// misread: wrong types, enum values the contract does not allow, missing
// required keys, and keys the contract does not document.

export interface ShapeIssue {
  readonly path: string;
  readonly problem: string;
}

type Schema = Record<string, unknown>;

const isObject = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

function typesOf(schema: Schema): readonly string[] {
  const t = schema.type;
  if (Array.isArray(t)) return t as string[];
  return typeof t === "string" ? [t] : [];
}

function matchesType(value: unknown, type: string): boolean {
  switch (type) {
    case "null":
      return value === null;
    case "string":
      return typeof value === "string";
    case "integer":
      return typeof value === "number" && Number.isInteger(value);
    case "number":
      return typeof value === "number";
    case "boolean":
      return typeof value === "boolean";
    case "array":
      return Array.isArray(value);
    case "object":
      return isObject(value);
    default:
      return true;
  }
}

export function validateShape(value: unknown, schema: unknown, path = "$"): ShapeIssue[] {
  const issues: ShapeIssue[] = [];
  check(value, schema, path, issues);
  return issues;
}

function check(value: unknown, raw: unknown, path: string, issues: ShapeIssue[]): void {
  if (!isObject(raw)) return;
  const schema = raw as Schema;
  if (value === null && (schema.nullable === true || typesOf(schema).includes("null"))) return;

  const variants = (schema.oneOf ?? schema.anyOf) as unknown[] | undefined;
  if (Array.isArray(variants)) {
    const passing = variants.filter((variant) => validateShape(value, variant, path).length === 0);
    if (passing.length === 0) {
      issues.push({ path, problem: "matches none of the contract's oneOf/anyOf variants" });
    }
    return;
  }
  if (Array.isArray(schema.allOf)) {
    const merged: Schema = { type: "object", properties: {}, required: [] };
    for (const part of schema.allOf as Schema[]) {
      Object.assign(merged.properties as Schema, (part.properties as Schema | undefined) ?? {});
      (merged.required as string[]).push(...((part.required as string[] | undefined) ?? []));
      if ("additionalProperties" in part) merged.additionalProperties = part.additionalProperties;
    }
    check(value, merged, path, issues);
    return;
  }

  const types = typesOf(schema);
  if (types.length > 0 && !types.some((type) => matchesType(value, type))) {
    issues.push({
      path,
      problem: `expected ${types.join("|")}, got ${value === null ? "null" : typeof value}`,
    });
    return;
  }
  if (Array.isArray(schema.enum) && !schema.enum.includes(value)) {
    issues.push({ path, problem: `${JSON.stringify(value)} is not an allowed value` });
  }
  if (Array.isArray(value)) {
    value.forEach((item, index) => check(item, schema.items, `${path}[${index}]`, issues));
    return;
  }
  if (!isObject(value)) return;

  const properties = (schema.properties as Schema | undefined) ?? {};
  for (const key of (schema.required as string[] | undefined) ?? []) {
    if (!(key in value))
      issues.push({ path: `${path}.${key}`, problem: "required by the contract but missing" });
  }
  const additional = schema.additionalProperties;
  for (const [key, child] of Object.entries(value)) {
    if (key in properties) {
      check(child, properties[key], `${path}.${key}`, issues);
    } else if (isObject(additional)) {
      check(child, additional, `${path}.${key}`, issues);
    } else if (additional !== true) {
      issues.push({ path: `${path}.${key}`, problem: "not documented by the contract" });
    }
  }
}
