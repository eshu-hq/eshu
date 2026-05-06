/**
 * Import/require patterns for parser testing.
 */

const { Animal, Dog } = require('./classes');
const { greet, compose, sum } = require('./functions');
const path = require('path');
const fs = require('fs');
const EventEmitter = require('events');

class AppService extends EventEmitter {
    constructor() {
        super();
        this.dog = new Dog('Rex');
    }

    start() {
        const greeting = greet(this.dog.name);
        this.emit('started', greeting);
        return greeting;
    }

    getConfigPath() {
        return path.join(__dirname, 'config.json');
    }

    calculate(...numbers) {
        const total = sum(...numbers);
        const doubled = compose(
            (x) => x * 2,
            (x) => x + 1
        )(total);
        return doubled;
    }
}

module.exports = { AppService };
