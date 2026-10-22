These three complete SSE responses were recorded on 2026-10-22 from
OpenRouter (`anthropic/claude-haiku-5.5`, Azure endpoint, reasoning high).
The `.sse.gz` files contain the exact original bytes, compressed with gzip.
The `.json` files are the independently assembled content at the transport
boundary. Source blocks are retained in `source.json`.

Original requests, usage and full audit:
`artifacts/cardsgen-audit-20261022/attempt-{4,6,8}/`.
No credentials or authorization headers are included.

Each response finished with `stop`, yet contained an unusable placeholder
choice, overlapping correct/incorrect answers and no flip card. This fixture
checks transport integrity and rejection, not model reliability or coverage.
