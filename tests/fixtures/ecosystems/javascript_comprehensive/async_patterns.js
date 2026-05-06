/**
 * Async patterns for parser testing.
 */

// Promise-based
function delay(ms) {
    return new Promise(resolve => setTimeout(resolve, ms));
}

// Async/await
async function fetchWithRetry(url, retries = 3) {
    for (let i = 0; i < retries; i++) {
        try {
            const response = await fetch(url);
            return await response.json();
        } catch (err) {
            if (i === retries - 1) throw err;
            await delay(1000 * (i + 1));
        }
    }
}

// Async generator
async function* paginate(fetchPage) {
    let page = 1;
    let hasMore = true;
    while (hasMore) {
        const result = await fetchPage(page);
        yield result.data;
        hasMore = result.hasMore;
        page++;
    }
}

// Promise.all pattern
async function fetchAll(urls) {
    const promises = urls.map(url => fetch(url).then(r => r.json()));
    return Promise.all(promises);
}

// Promise.race pattern
async function fetchWithTimeout(url, timeout = 5000) {
    return Promise.race([
        fetch(url),
        delay(timeout).then(() => { throw new Error('Timeout'); })
    ]);
}

module.exports = { delay, fetchWithRetry, paginate, fetchAll, fetchWithTimeout };
