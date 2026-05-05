/**
 * Decorator patterns for parser testing.
 */

// Class decorator
function Component(selector: string) {
    return function <T extends { new (...args: any[]): {} }>(constructor: T) {
        return class extends constructor {
            selector = selector;
        };
    };
}

// Method decorator
function Log(
    target: any,
    propertyKey: string,
    descriptor: PropertyDescriptor,
): PropertyDescriptor {
    const original = descriptor.value;
    descriptor.value = function (...args: any[]) {
        console.log(`Calling ${propertyKey} with`, args);
        return original.apply(this, args);
    };
    return descriptor;
}

// Property decorator
function Required(target: any, propertyKey: string) {
    let value: any;
    Object.defineProperty(target, propertyKey, {
        get: () => value,
        set: (newValue) => {
            if (newValue === undefined || newValue === null) {
                throw new Error(`${propertyKey} is required`);
            }
            value = newValue;
        },
    });
}

// Parameter decorator
function Validate(
    target: any,
    propertyKey: string,
    parameterIndex: number,
) {
    const validators = Reflect.getMetadata("validators", target, propertyKey) || [];
    validators.push(parameterIndex);
    Reflect.defineMetadata("validators", validators, target, propertyKey);
}

@Component("app-root")
class AppComponent {
    @Required
    title: string;

    constructor(title: string) {
        this.title = title;
    }

    @Log
    greet(@Validate name: string): string {
        return `Hello, ${name}!`;
    }
}

export { Component, Log, Required, Validate, AppComponent };
