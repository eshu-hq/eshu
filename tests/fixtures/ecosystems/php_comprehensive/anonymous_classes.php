<?php
namespace Comprehensive;

interface Handler {
    public function handle(): void;
}

class Dispatcher {
    public function register(): Handler {
        return new class implements Handler {
            public function handle(): void {
                echo "handled";
            }
        };
    }

    public function registerSecond(): Handler {
        return new class implements Handler {
            public function handle(): void {
                echo "handled again";
            }
        };
    }
}
