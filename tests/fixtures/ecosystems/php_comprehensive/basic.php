<?php
namespace Comprehensive;

function greet(string $name): string {
    return "Hello, {$name}!";
}

function add(int $a, int $b): int {
    return $a + $b;
}

class Config {
    private string $env;
    private bool $debug;

    public function __construct(string $env = 'development') {
        $this->env = $env;
        $this->debug = $env !== 'production';
    }

    public function getEnv(): string {
        return $this->env;
    }

    public function isDebug(): bool {
        return $this->debug;
    }
}
