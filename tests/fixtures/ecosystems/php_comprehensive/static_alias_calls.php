<?php
namespace Comprehensive;

use Comprehensive\Factory as AppFactory;

class Service {
    public function info(string $message): void {}
}

class Factory {
    public static function instance(): Factory {
        return new Factory();
    }

    public function createService(): Service {
        return new Service();
    }
}

class Application {
    public function run(): void {
        AppFactory::instance()->createService()->info('ready');
    }
}
