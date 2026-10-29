# PHP configuration parser guidance

Keep parsing bounded, deterministic, and purely lexical. Never evaluate INI, Apache, or PHP-FPM expressions. Never include rejected source text or secret-shaped values in a result, error, log, or test failure. The parent parser owns registry admission, content-store policy, and query surfaces. Before changing emitted fields, trace the `variables` bucket through content-entity materialization and the PHP configuration read route.
