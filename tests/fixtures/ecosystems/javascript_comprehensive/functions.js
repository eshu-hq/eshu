/**
 * Function patterns for parser testing.
 */

// Regular function declaration
function greet(name) {
    return `Hello, ${name}!`;
}

// Arrow function
const double = (x) => x * 2;

// Arrow function with body
const processItems = (items) => {
    const results = [];
    for (const item of items) {
        results.push(item.toUpperCase());
    }
    return results;
};

// Generator function
function* range(start, end, step = 1) {
    for (let i = start; i < end; i += step) {
        yield i;
    }
}

// Async function
async function fetchData(url) {
    const response = await fetch(url);
    return response.json();
}

// IIFE
const config = (() => {
    const settings = { debug: false, version: '1.0.0' };
    return Object.freeze(settings);
})();

// Higher-order function
function compose(...fns) {
    return (x) => fns.reduceRight((acc, fn) => fn(acc), x);
}

// Destructuring parameters
function createUser({ name, age, email = 'none' }) {
    return { name, age, email, createdAt: new Date() };
}

// Rest parameters
function sum(...numbers) {
    return numbers.reduce((acc, n) => acc + n, 0);
}

// Default parameters
function connect(host = 'localhost', port = 3000) {
    return { host, port };
}

module.exports = {
    greet,
    double,
    processItems,
    range,
    fetchData,
    compose,
    createUser,
    sum,
    connect,
};
