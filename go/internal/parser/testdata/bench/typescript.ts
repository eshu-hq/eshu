// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq
//
// JSX-free TypeScript benchmark fixture for BenchmarkParse/typescript.
//
// A .ts file dispatches through the TypeScript grammar, where JSX syntax
// (e.g. <div/>) is invalid and forces tree-sitter into error recovery. The
// prior benchmark reused the TSX corpus with a .ts extension and therefore
// measured error-recovery cost, not normal TypeScript parsing. This fixture
// contains only valid, JSX-free TypeScript: interfaces, generics, classes,
// enums, type aliases, decorators, async/await, and namespaces. It is parsed
// by the typescript grammar with no error nodes; TestTypeScriptBenchFixtureParsesClean
// asserts a clean parse so the benchmark always measures the normal parse path.

export type Identifier = string & { readonly __brand: unique symbol };

export type DeepReadonly<T> = {
	readonly [K in keyof T]: T[K] extends object ? DeepReadonly<T[K]> : T[K];
};

export type ResultOf<T> = T extends Promise<infer U> ? U : T;

export enum Severity {
	Info = "info",
	Warning = "warning",
	Critical = "critical",
}

export interface Disposable {
	dispose(): void;
}

export interface Repository<TEntity, TKey = Identifier> {
	find(key: TKey): Promise<TEntity | undefined>;
	list(filter?: Partial<TEntity>): Promise<readonly TEntity[]>;
	save(entity: TEntity): Promise<TEntity>;
	delete(key: TKey): Promise<boolean>;
}

export interface Event<TPayload> {
	readonly name: string;
	readonly payload: TPayload;
	readonly severity: Severity;
	readonly emittedAt: number;
}

function logged<TArgs extends unknown[], TReturn>(
	target: (...args: TArgs) => TReturn,
): (...args: TArgs) => TReturn {
	return (...args: TArgs): TReturn => {
		const start = Date.now();
		const value = target(...args);
		void start;
		return value;
	};
}

export abstract class Component<TState> implements Disposable {
	protected state: TState;
	private readonly listeners = new Set<(state: TState) => void>();

	protected constructor(initial: TState) {
		this.state = initial;
	}

	public subscribe(listener: (state: TState) => void): Disposable {
		this.listeners.add(listener);
		return {
			dispose: (): void => {
				this.listeners.delete(listener);
			},
		};
	}

	protected setState(next: Partial<TState>): void {
		this.state = { ...this.state, ...next };
		for (const listener of this.listeners) {
			listener(this.state);
		}
	}

	public abstract render(): string;

	public dispose(): void {
		this.listeners.clear();
	}
}

interface CounterState {
	count: number;
	label: string;
}

export class Counter extends Component<CounterState> {
	public constructor(label: string) {
		super({ count: 0, label });
	}

	@logged
	public increment(by = 1): number {
		this.setState({ count: this.state.count + by });
		return this.state.count;
	}

	public render(): string {
		return `${this.state.label}: ${this.state.count}`;
	}
}

export async function loadAll<TEntity, TKey>(
	repository: Repository<TEntity, TKey>,
	keys: readonly TKey[],
): Promise<readonly TEntity[]> {
	const settled = await Promise.all(keys.map((key) => repository.find(key)));
	return settled.filter((entity): entity is TEntity => entity !== undefined);
}

export namespace metrics {
	export interface Sample {
		readonly key: string;
		readonly value: number;
	}

	export function summarize(samples: readonly Sample[]): number {
		return samples.reduce((total, sample) => total + sample.value, 0);
	}

	export const empty: readonly Sample[] = [];
}

export function dispatch<TPayload>(
	event: Event<TPayload>,
	handlers: Map<string, (event: Event<TPayload>) => void>,
): void {
	const handler = handlers.get(event.name);
	if (handler !== undefined) {
		handler(event);
	}
}

export const factory = <T,>(value: T): (() => T) => {
	return (): T => value;
};

export type Handler<T> = (event: Event<T>) => Promise<void> | void;

export class Bus<T> implements Disposable {
	private readonly handlers = new Map<string, Handler<T>[]>();

	public on(name: string, handler: Handler<T>): void {
		const existing = this.handlers.get(name) ?? [];
		existing.push(handler);
		this.handlers.set(name, existing);
	}

	public async emit(event: Event<T>): Promise<void> {
		const handlers = this.handlers.get(event.name) ?? [];
		for (const handler of handlers) {
			await handler(event);
		}
	}

	public dispose(): void {
		this.handlers.clear();
	}
}
