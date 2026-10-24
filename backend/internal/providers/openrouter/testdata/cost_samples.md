# Offline cost reservation corpus

`cost_samples.json` contains 28 numeric observations from the local `artifacts`
directory, recorded on 2026-10-22 through 2026-10-24. Tests use this committed
snapshot without credentials, original files, external processes or network access.
Prompts, PDF bytes, filenames, user identifiers and generation identifiers are omitted.

## Provenance and limitations

The first 27 observations pair a recorded HTTP `request.json` with its
`api-usage.json`. Some usage files wrap the counters inside `usage`; the others
contain the counters directly. Generation identifiers from the response SSE,
generation lookup or summary were used to avoid counting the same billed request
twice. Summaries of entire experiments, aggregate stage usage, unknown charges and
rejected requests without reported tokens are not calibration samples.

| IDs | Source directories under `artifacts/` | Source group |
| --- | --- | --- |
| recorded-01–06 | `cardsgen-audit-20261022/attempt-3` through `attempt-8` | text-small |
| recorded-07–15 | `cardsgen-fix-20261022/`: baseline-1/2, candidate-1/2, diagnostic-endpoint-anthropic-corrected/repeat, diagnostic-reasoning-none, final-1/2 | text-small |
| recorded-16–17 | `lecture2-set-20261022/attempt-1` and `attempt-2` | text-lecture2 |
| recorded-18–21 | `lecture2-v5-set-20261022/attempt-1` through `attempt-4` | text-lecture2 |
| recorded-22–24 | `pdf-direct-20261023/`: baseline-cached-reading, baseline-final, baseline-medium | text-indexing-reading |
| recorded-25–27 | `pdf-direct-20261023/`: native-1, native-2, native-none | native-indexing |
| current-pipeline-check | `indexing-openrouter-review/quote-check-removal-20261024/live-3.json` | native-indexing |

For HTTP captures, `textBytes` sums UTF-8 lengths of message strings, text content
parts and compact JSON Schema. File captions and repeated task messages count;
base64 file data does not. Native PDF metadata was recovered from the captured
PDF using Poppler `pdfinfo` and `pdftotext -layout -enc UTF-8 -eol unix`; form-feed
page separators are excluded, matching the ingestion pipeline's `PDF.TextBytes`.
For `current-pipeline-check`, the recorded `artifact.request` and PDF metadata were
used, including the source instructions and file captions added by the transport.
Its tokens and cost come from the single generation stage, not aggregate totals.

All four native observations are the **same 27-page PDF**, with 26,105 text bytes.
Repeated requests are separate billing observations, not independent source
documents. The current-pipeline observation checks a later prompt against that
same PDF; it is not independent PDF validation. The 53-page lecture has historical
text-only usage, but no captured native-PDF usage in this corpus. Its native cost
remains an extrapolation. Output samples include truncated and invalid generations
because those still incur costs; truncated lengths are not output-length targets.

## Fixed policy: haiku-5.5-cost-v1

Apply only to `anthropic/claude-haiku-5.5` (including `:` variants), without images,
and with positive `textBytes` for every supplied PDF. Other models, rolling aliases,
images, scans and missing PDF text metadata retain the conservative input allowance.

```text
B = UTF-8 bytes of prepared text messages/parts plus JSON Schema
T = sum of extracted PDF text bytes
P = sum of native PDF pages

estimated input tokens = ceil((B + T) / 3) + 1536 * P + 1024
reserved input tokens  = ceil(estimated input tokens * 1.25)
reserved USD          = reserved input tokens * 0.10 / 1e6 * cache multiplier
                      + sent max_tokens * 0.50 / 1e6
```

The byte divisor approximates the observed text token density. The rounded page
allowance covers the native PDF overhead remaining after text accounting in the
recorded sample. The 1,024-token allowance covers message/schema overhead; the
additional 25% input reserve covers estimation uncertainty. These are fixed,
empirical coefficients, not a fitted statistical confidence interval.

The cache multiplier is 1.25 when the prepared request has a `cache_control`
breakpoint, otherwise 1. It reserves a cold five-minute cache write, even when a
cache hit might make the request cheaper. This price markup is separate from the
25% token uncertainty reserve. Output reserves the full **sent** limit, including
reasoning; a request override replaces the client default in both places.

## Offline comparison

| Group | Observations | New reserved input / reported input |
| --- | ---: | ---: |
| text-small | 15 | 1.53–1.64 |
| text-lecture2 | 6 | 1.56–1.69 |
| text-indexing-reading | 3 | 1.28 |
| native-indexing | 4 | 1.33–1.34 |

No recorded observation has under-reserved input tokens or cost. This describes
the corpus only; it does not establish coverage for unseen documents.

For a comparable prepared text payload of about 20 KB, a cold cache and the full
128,000-token output ceiling:

| PDF metadata | Previous reservation | New reservation |
| --- | ---: | ---: |
| 45 pages, 24,519 extracted text bytes | about $0.162 | about $0.077 |
| 53 pages, 35,498 extracted text bytes | about $0.180 | about $0.080 |

These rows use local document metadata, not paid native generations. Regression
tests verify admission under the existing $0.15 budget through an HTTP stub.

The ledger still settles reported `usage.cost`, retains unknown reservations,
counts transport retries and serializes concurrent reservations. An actual charge
above its reservation stops subsequent attempts as before. The monetary limit is
an admission threshold based on an estimate, not a guaranteed billing ceiling.
There is no runtime training, new database storage or additional API integration.

Numeric `slog` diagnostics include `cost_estimate_version`, input byte/page counts,
estimated/reserved input tokens, `max_output_tokens` and `reserved_cost_usd`.
Reported input above its reservation produces an underestimation warning. Review
those discrepancies before changing the fixed policy or enabling it for another model.
