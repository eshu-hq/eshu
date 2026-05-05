/**
 * Type alias patterns for parser testing.
 */

// Union types
type StringOrNumber = string | number;
type Primitive = string | number | boolean | null | undefined;
type Status = "pending" | "active" | "inactive" | "deleted";

// Intersection types
type WithTimestamp = { createdAt: Date; updatedAt: Date };
type WithId = { id: string };
type Entity = WithId & WithTimestamp;

// Conditional types
type NonNullable<T> = T extends null | undefined ? never : T;
type Awaited<T> = T extends Promise<infer U> ? U : T;

// Template literal types
type HttpMethod = "GET" | "POST" | "PUT" | "DELETE" | "PATCH";
type ApiEndpoint = `/${string}`;
type Route = `${HttpMethod} ${ApiEndpoint}`;

// Utility types
type DeepPartial<T> = {
    [P in keyof T]?: T[P] extends object ? DeepPartial<T[P]> : T[P];
};

type Mutable<T> = {
    -readonly [P in keyof T]: T[P];
};

// Record type usage
type ErrorMessages = Record<Status, string>;

// Tuple types
type Pair<A, B> = [A, B];
type Triple<A, B, C> = [A, B, C];
type Coordinate = [number, number];

// Function types
type Handler<T> = (event: T) => void;
type AsyncHandler<T> = (event: T) => Promise<void>;
type Transformer<In, Out> = (input: In) => Out;

export type {
    StringOrNumber,
    Status,
    Entity,
    HttpMethod,
    Route,
    DeepPartial,
    Handler,
    Transformer,
};
