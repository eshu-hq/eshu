/**
 * Class patterns for parser testing.
 */

class Animal {
    #name;

    constructor(name, species) {
        this.#name = name;
        this.species = species;
    }

    get name() {
        return this.#name;
    }

    set name(value) {
        this.#name = value;
    }

    speak() {
        return `${this.#name} makes a sound`;
    }

    static create(name, species) {
        return new Animal(name, species);
    }
}

class Dog extends Animal {
    constructor(name) {
        super(name, 'Canine');
    }

    speak() {
        return `${this.name} barks`;
    }

    fetch(item) {
        return `${this.name} fetches ${item}`;
    }
}

class Cat extends Animal {
    constructor(name) {
        super(name, 'Feline');
    }

    speak() {
        return `${this.name} meows`;
    }
}

module.exports = { Animal, Dog, Cat };
