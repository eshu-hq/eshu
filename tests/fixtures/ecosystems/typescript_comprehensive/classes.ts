/**
 * Class patterns for parser testing.
 */

abstract class Shape {
    abstract area(): number;
    abstract perimeter(): number;

    describe(): string {
        return `Shape with area ${this.area()}`;
    }
}

class Circle extends Shape {
    constructor(private radius: number) {
        super();
    }

    area(): number {
        return Math.PI * this.radius ** 2;
    }

    perimeter(): number {
        return 2 * Math.PI * this.radius;
    }
}

class Rectangle extends Shape {
    constructor(
        private width: number,
        private height: number,
    ) {
        super();
    }

    area(): number {
        return this.width * this.height;
    }

    perimeter(): number {
        return 2 * (this.width + this.height);
    }
}

class Animal {
    static count = 0;

    #name: string;
    protected species: string;

    constructor(name: string, species: string) {
        this.#name = name;
        this.species = species;
        Animal.count++;
    }

    get name(): string {
        return this.#name;
    }

    set name(value: string) {
        this.#name = value;
    }

    toString(): string {
        return `${this.#name} (${this.species})`;
    }
}

class Dog extends Animal {
    constructor(name: string) {
        super(name, "Canine");
    }

    bark(): string {
        return "Woof!";
    }
}

export { Shape, Circle, Rectangle, Animal, Dog };
