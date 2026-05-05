<?php
namespace Comprehensive;

use Comprehensive\Config;
use Comprehensive\Service;
use Comprehensive\Circle;
use Comprehensive\Rectangle;

require_once __DIR__ . '/basic.php';
require_once __DIR__ . '/classes.php';
require_once __DIR__ . '/traits.php';

class Application {
    private Config $config;
    private Service $service;

    public function __construct() {
        $this->config = new Config('production');
        $this->service = new Service('main');
    }

    public function run(): void {
        $greeting = greet('World');
        $this->service->info($greeting);

        $shapes = [new Circle(5.0), new Rectangle(3.0, 4.0)];
        foreach ($shapes as $shape) {
            $this->service->info($shape->describe());
        }
    }
}
