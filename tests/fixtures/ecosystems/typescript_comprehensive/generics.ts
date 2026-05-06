/**
 * Generic patterns for parser testing.
 */

function identity<T>(value: T): T {
    return value;
}

function first<T>(items: T[]): T | undefined {
    return items[0];
}

function merge<T extends object, U extends object>(a: T, b: U): T & U {
    return { ...a, ...b };
}

class Result<T, E extends Error = Error> {
    private constructor(
        private readonly value?: T,
        private readonly error?: E,
    ) {}

    static ok<T>(value: T): Result<T, never> {
        return new Result(value);
    }

    static err<E extends Error>(error: E): Result<never, E> {
        return new Result(undefined, error);
    }

    map<U>(fn: (value: T) => U): Result<U, E> {
        if (this.value !== undefined) {
            return Result.ok(fn(this.value));
        }
        return Result.err(this.error!);
    }

    unwrap(): T {
        if (this.error) throw this.error;
        return this.value!;
    }
}

// Mapped types
type Readonly<T> = { readonly [P in keyof T]: T[P] };
type Partial<T> = { [P in keyof T]?: T[P] };
type Required<T> = { [P in keyof T]-?: T[P] };

// Conditional types
type IsString<T> = T extends string ? true : false;
type ElementType<T> = T extends (infer E)[] ? E : T;
type ReturnTypeOf<T> = T extends (...args: any[]) => infer R ? R : never;

export { identity, first, merge, Result };
