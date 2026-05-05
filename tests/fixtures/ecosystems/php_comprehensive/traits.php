<?php
namespace Comprehensive;

trait Loggable {
    public function log(string $level, string $message): void {
        echo "[{$level}] {$message}\n";
    }

    public function info(string $message): void {
        $this->log('INFO', $message);
    }

    public function warn(string $message): void {
        $this->log('WARN', $message);
    }
}

trait Serializable {
    public function toArray(): array {
        return get_object_vars($this);
    }

    public function toJson(): string {
        return json_encode($this->toArray());
    }
}

class Service implements Logger {
    use Loggable;
    use Serializable;

    private string $name;

    public function __construct(string $name) {
        $this->name = $name;
    }

    public function error(string $message): void {
        $this->log('ERROR', $message);
    }

    public function getName(): string {
        return $this->name;
    }
}
