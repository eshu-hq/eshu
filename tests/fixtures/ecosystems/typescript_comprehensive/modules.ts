/**
 * Module patterns for parser testing.
 */

// Named imports
import { Shape, Circle } from "./classes";
import type { Serializable, Logger } from "./interfaces";
import { identity } from "./generics";

// Default import
import defaultExport from "./classes";

// Namespace import
import * as Types from "./type_aliases";

// Re-exports
export { Circle } from "./classes";
export { identity } from "./generics";
export type { Repository } from "./interfaces";

// Named exports
export const VERSION = "1.0.0";

export function createCircle(radius: number): Circle {
    return new Circle(radius);
}

// Default export
export default class ModuleManager {
    private modules: Map<string, unknown> = new Map();

    register(name: string, module: unknown): void {
        this.modules.set(name, module);
    }

    get<T>(name: string): T | undefined {
        return this.modules.get(name) as T | undefined;
    }
}
