# PHP configuration parser

This package reads PHP INI files, `.htaccess` overrides, Apache module declarations, and PHP-FPM pool declarations during repository parsing. It returns directive name, value, section, and source line, plus declared server modes. These are source declarations, not observed runtime settings.

Input is capped at 1 MiB and 2 KiB per line. At most 256 accepted rows are kept. Invalid and credential-shaped lines produce bounded reason counts without retaining their text. The parent parser writes accepted rows as `variables` with `source_cache_withheld`; the PHP configuration registry entry uses the metadata-only content-store policy, so raw file bodies are not persisted in the content store. Filesystem sync keeps a managed repository copy of `.htaccess` until the next workspace refresh so parsing can read it.

`php.ini`, `.htaccess`, `httpd.conf`, and `apache2.conf` bind by exact name. `conf.d/*.ini`, `php-fpm.d/*.conf`, and `.conf` files in Apache directories bind by path. Generic `.ini` and `.conf` files are not promoted to PHP configuration.
