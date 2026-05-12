<?php

interface PhpRenderable {
    public function render(): string;
}

trait PublicPhpTrait {
    public function bootPublicPhpController(): void {
    }
}

function main(): void {
    directPhpHelper();
    selectedPhpHandler();
    (new PublicPhpController())->indexAction();
}

function unusedPhpHelper(): string {
    return 'unused';
}

function directPhpHelper(): string {
    return 'direct';
}

class PublicPhpController implements PhpRenderable {
    use PublicPhpTrait;

    public function __invoke(): string {
        return 'invoke';
    }

    public function render(): string {
        return 'render';
    }

    public function indexAction(): string {
        return directPhpHelper();
    }

    private function helper(): string {
        return 'helper';
    }
}

function selectedPhpHandler(): string {
    return directPhpHelper();
}

function generatedPhpStub(): string {
    return 'generated';
}

function dynamicPhpDispatch(string $name): mixed {
    return $name();
}

add_action('init', 'selectedPhpHandler');
Route::get('/public', [PublicPhpController::class, 'indexAction']);

main();
