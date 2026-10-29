# PHP Configuration Search

`POST /api/v0/content/php-config/search` accepts exactly one of `directive`
(for example `memory_limit`) or `mode` (`mod_php` or `php-fpm`), plus an optional
`repo_id`, `limit` (default 50, maximum 200), and opaque `cursor`. It returns
`results`, `count`, `limit`, `truncated`, and `next_cursor` when another page
exists. Repeat the same filter with `cursor=next_cursor` to enumerate the full
ordered match set across repositories. The cursor is bound to the directive or
mode and repository filter; repository grants are applied before each page.
Concurrent reindexing can change the result set between pages.
Credential-shaped directive names are rejected before the content index is read.

Each result is an indexed declaration with `repo_id`, `relative_path`,
`start_line`, `entity_name`, and guarded `metadata.value`. Mode evidence comes
from final-stage PHP Dockerfile base tags with `apache` or `fpm` tag components,
`a2enmod php*` normalized install rows, Apache `LoadModule php_module`, and
PHP-FPM pool declarations. These are committed source declarations, not a
runtime observation. A repository can declare more than one mode; the route
returns both rather than choosing an effective mode.

`php.ini`, `conf.d/*.ini`, `.htaccess`, Apache config files, and FPM pool files
use the metadata-only content-store policy. Their raw bodies and entity
`source_cache` are withheld from the content store and this response. Unsafe
or unparsable values are dropped with counts in parser output. In filesystem
sync mode, the managed repository workspace holds a local copy of `.htaccess` so
the parser can read it; that copy remains until the next workspace refresh. It is not a content-store row.

No-Observability-Change: this route uses the shared HTTP handler span and the
`postgres.query` span with `db.operation=search_php_config_entities`. It
passes user-supplied values as SQL parameters and emits no source body to
logs, metrics, or span attributes.
