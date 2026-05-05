/**
 * Interface patterns for parser testing.
 */

interface Serializable {
    serialize(): string;
    deserialize(data: string): void;
}

interface Identifiable {
    readonly id: string;
    readonly createdAt: Date;
}

interface Repository<T extends Identifiable> {
    findById(id: string): Promise<T | null>;
    findAll(): Promise<T[]>;
    save(entity: T): Promise<T>;
    delete(id: string): Promise<boolean>;
}

interface EventEmitter {
    on(event: string, listener: (...args: any[]) => void): void;
    emit(event: string, ...args: any[]): void;
    off(event: string, listener: (...args: any[]) => void): void;
}

interface Configurable {
    configure(options: Record<string, unknown>): void;
}

interface Logger {
    info(message: string, ...meta: any[]): void;
    warn(message: string, ...meta: any[]): void;
    error(message: string, ...meta: any[]): void;
}

// Extended interface
interface Service extends Configurable, Logger {
    start(): Promise<void>;
    stop(): Promise<void>;
    health(): { status: string; uptime: number };
}

// Interface with optional and readonly
interface UserProfile {
    readonly id: string;
    username: string;
    email: string;
    bio?: string;
    avatar?: string;
    settings: {
        theme: "light" | "dark";
        notifications: boolean;
    };
}

export type {
    Serializable,
    Identifiable,
    Repository,
    EventEmitter,
    Service,
    UserProfile,
    Logger,
};
