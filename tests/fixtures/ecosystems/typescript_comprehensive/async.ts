/**
 * Async patterns for parser testing.
 */

async function fetchData(url: string): Promise<Record<string, unknown>> {
    const response = await fetch(url);
    return response.json();
}

async function processInBatch<T>(
    items: T[],
    batchSize: number,
    processor: (item: T) => Promise<void>,
): Promise<void> {
    for (let i = 0; i < items.length; i += batchSize) {
        const batch = items.slice(i, i + batchSize);
        await Promise.all(batch.map(processor));
    }
}

function* range(start: number, end: number, step = 1): Generator<number> {
    for (let i = start; i < end; i += step) {
        yield i;
    }
}

async function* asyncRange(
    start: number,
    end: number,
): AsyncGenerator<number> {
    for (let i = start; i < end; i++) {
        await new Promise((resolve) => setTimeout(resolve, 10));
        yield i;
    }
}

class AsyncQueue<T> {
    private queue: T[] = [];

    async enqueue(item: T): Promise<void> {
        this.queue.push(item);
    }

    async dequeue(): Promise<T | undefined> {
        return this.queue.shift();
    }

    async process(handler: (item: T) => Promise<void>): Promise<void> {
        while (this.queue.length > 0) {
            const item = this.queue.shift();
            if (item !== undefined) {
                await handler(item);
            }
        }
    }
}

export { fetchData, processInBatch, range, asyncRange, AsyncQueue };
