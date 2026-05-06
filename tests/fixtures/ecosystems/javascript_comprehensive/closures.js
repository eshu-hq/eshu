/**
 * Closure patterns for parser testing.
 */

// Counter closure
function createCounter(initial = 0) {
    let count = initial;
    return {
        increment: () => ++count,
        decrement: () => --count,
        getCount: () => count,
        reset: () => { count = initial; }
    };
}

// Factory function
function createLogger(prefix) {
    return {
        info: (msg) => console.log(`[${prefix}] INFO: ${msg}`),
        warn: (msg) => console.warn(`[${prefix}] WARN: ${msg}`),
        error: (msg) => console.error(`[${prefix}] ERROR: ${msg}`),
    };
}

// Memoization
function memoize(fn) {
    const cache = new Map();
    return function(...args) {
        const key = JSON.stringify(args);
        if (cache.has(key)) return cache.get(key);
        const result = fn.apply(this, args);
        cache.set(key, result);
        return result;
    };
}

// Module pattern
const Calculator = (() => {
    let history = [];

    function add(a, b) {
        const result = a + b;
        history.push({ op: 'add', a, b, result });
        return result;
    }

    function getHistory() {
        return [...history];
    }

    return { add, getHistory };
})();

module.exports = { createCounter, createLogger, memoize, Calculator };
