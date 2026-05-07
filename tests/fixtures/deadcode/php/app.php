<?php

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

class PublicPhpController {
    public function indexAction(): string {
        return directPhpHelper();
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

main();
