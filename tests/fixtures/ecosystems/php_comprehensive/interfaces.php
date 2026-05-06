<?php
namespace Comprehensive;

interface Identifiable {
    public function getId(): string;
}

interface Describable {
    public function describe(): string;
}

interface Repository {
    public function findById(string $id): ?object;
    public function findAll(): array;
    public function save(object $entity): void;
    public function delete(string $id): bool;
}

interface Logger {
    public function info(string $message): void;
    public function warn(string $message): void;
    public function error(string $message): void;
}
