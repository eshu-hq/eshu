# Source Cache Read Clip

The routes that return entity rows carrying a stored source body clip that body
at read time so one oversized entity cannot push a page over the MCP response
budget. The clip is a response-shaping rule; it never changes what is stored.

| Route | MCP tool |
| --- | --- |
| `POST /api/v0/code/symbols/search` | `find_symbol` |
| `POST /api/v0/code/structure/inventory` | `inspect_code_inventory` |
| `POST /api/v0/code/search` (content-store rows) | `find_code` |
| `POST /api/v0/content/entities/search` | `search_entity_content` |

## Contract

- Each row's `source_cache` is cut to at most **4,096 bytes**. The cut never
  splits a UTF-8 code point, so a clipped body can be shorter than 4,096 bytes
  by up to three bytes.
- A clipped row carries three sparse markers, and only a clipped row does:
  `source_cache_clipped: true`, `source_cache_clip_bytes: 4096`, and
  `source_cache_total_bytes` (the stored length before the clip).
- Every response of these routes carries `source_cache_clip_bytes: 4096` and
  `source_cache_clipped_rows` (`0` when no row was clipped), beside `count`,
  `limit`, and `truncated`. The count covers the rows in the returned page.
- These markers are distinct from the write-time
  `metadata.source_cache_truncated` family, which records a lossy cut made when
  the body was stored (variables at 4,096 bytes, GitHub Actions files at
  32 KiB). A row can carry both.
- Ranking is unaffected: the hybrid re-rank of `search_entity_content` and
  `find_code` reads the full stored body first and the clip is applied to the
  page it returns.

## Drill-down

The full body is one call away. `find_symbol`, `inspect_code_inventory`, and
`search_entity_content` rows carry `source_handle`
(`{repo_id, file_path, start_line, end_line}`); pass the row's `entity_id` to
`get_entity_content` (`POST /api/v0/content/entities/read`, never clipped) or
the handle's range to `get_file_lines`. `find_code` rows carry no `source_handle`;
their `entity_id` works with `get_entity_content` the same way.

## Defaults

The MCP defaults are `find_symbol` 20 rows, `inspect_code_inventory` 20 rows,
and `search_entity_content` 10 rows. At these defaults a reply stays under the
256 KiB dispatch budget when each row is a clipped `source_cache` (4,096 bytes)
plus about 1.5 KB of other row content. Only `source_cache` is clipped:
`metadata` (a long `docstring` is echoed about five times in a symbol or
inventory row) is returned whole, so rows with large metadata can still push a
default-args reply over the budget. Such a reply fails closed with
`mcp_response_over_budget`, whose guidance names `get_entity_content` for a
clipped row. Bounding row metadata is open work in
[#7234](https://github.com/eshu-hq/eshu/issues/7234). The HTTP handler defaults are
unchanged (`symbols/search` 25, `structure/inventory` 25, `content/entities/search`
50), so a direct HTTP caller that asks for more rows than the MCP default is
responsible for its own page size.
