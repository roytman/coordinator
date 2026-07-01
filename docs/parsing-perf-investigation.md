# Request-body parsing: performance investigation

## Question

Should the coordinator change how it represents the parsed request body to
improve performance? Candidates considered:

1. `map[string]any` (current)
2. `map[string]json.RawMessage`
3. Hybrid struct (typed hot fields + `json.RawMessage` catch-all)
4. Patch the cached original body bytes instead of re-marshaling
5. A faster JSON library (`bytedance/sonic`, `goccy/go-json`,
   `valyala/fastjson`)

## TL;DR

- **Do not** switch `map[string]any` to `map[string]json.RawMessage`. It is
  measurably **slower** on the multimodal path (~2x) and only marginally leaner
  on text.
- **Do not** adopt a helper struct for the body. For an open OpenAI-style schema
  it is slower still on the round-trip and cannot represent arbitrary
  passthrough fields.
- The byte-patching idea is the fastest in a microbenchmark, but **does not
  apply** to the path where the cost actually lives (see "Why byte-patching does
  not help").
- A faster JSON library **is** a real win on the multimodal path: `sonic` ~14x,
  `fastjson` ~27x on the realistic parse + mutate + serialize workload. But it
  is a CPU/throughput win, not a user-visible latency win, and each carries
  adoption cost (see "JSON library comparison").
- Net conclusion: **the body representation is not where coordinator
  performance lives.** Per-request JSON handling is microseconds-to-~1ms against
  multi-second GPU inference. A faster library only matters if profiling proves
  the router is CPU-bound on JSON under multimodal load. Absent that evidence,
  no change is recommended.

## How it was measured

A standalone benchmark parsed a representative body, read the top-level fields
the router uses (`model`, `stream`), then re-marshaled it for forwarding (what
the pipeline does today). Two bodies:

- **Text-only** chat request, ~430 bytes, several sampling params the router
  forwards untouched.
- **Multimodal** request, ~340 KB, with a base64 image inlined in
  `messages[].content[].image_url.url`.

Run on Apple M4 Max, Go 1.25, `-benchmem`. Numbers are representative medians.

### Text-only body (~430 B)

| Strategy                       | ns/op | B/op | allocs/op |
| ------------------------------ | ----- | ---- | --------- |
| `map[string]any` (current)     | 3696  | 3003 | 81        |
| `map[string]json.RawMessage`   | 3759  | 2428 | 55        |
| Hybrid struct                  | 6866  | 3872 | 66        |
| Patch + scalar re-parse        | 2542  | 2507 | 26        |
| Patch only (no re-parse)       | 925   | 2186 | 18        |

### Multimodal body (~340 KB base64 image)

| Strategy                       | ns/op     | B/op    | allocs/op |
| ------------------------------ | --------- | ------- | --------- |
| `map[string]any` (current)     | 1,099,870 | 729 KB  | 85        |
| `map[string]json.RawMessage`   | 2,017,459 | 717 KB  | 41        |
| Hybrid struct                  | 4,026,992 | 1.08 MB | 52        |
| Patch + scalar re-parse        | 1,230,430 | 798 KB  | 27        |
| Patch only (no re-parse)       | 56,223    | 796 KB  | 18        |

## Why `map[string]json.RawMessage` is slower

The intuition is that `RawMessage` lets untouched fields pass through "for
free." That is false on **marshal**. When `encoding/json` marshals a
`json.RawMessage`, it re-validates and compacts every raw byte
(`appendCompact` + `checkValid`). So the 340 KB image string is walked
byte-by-byte on the way out, on top of the unmarshal that already produced the
raw slices. The current `map[string]any` does not pay that re-validation pass,
which is why it is faster at forwarding a body containing a large string.

CPU profile of the multimodal `RawMessage` case: the marshal step alone is the
single largest cost, dominated by `json.appendCompact` / `json.checkValid` /
`json.stateInString`.

Allocations do drop with `RawMessage` (85 to 41), but allocations are not the
cost on this path; byte-scanning the image is.

## Why byte-patching does not help in practice

"Patch only" (56 KB image, 19x faster than current) is the fastest row, but it
relies on an assumption that is false for the real pipeline: that the cached
original body bytes are still valid at forward time and only top-level keys are
added.

In the actual multimodal flow, the body is mutated in **nested** positions and
is partly **constructed mid-pipeline**:

- `replace-media-urls` rewrites `messages[].content[].image_url.url` in place:
  a remote URL is downloaded and inlined as a base64 data URI. The large body
  is not the original at all; it is built during the request.
- `decode` adds a `uuid` field to each nested image part.

Byte-splicing can only insert **top-level** keys onto an otherwise-untouched
body. It cannot perform nested edits without parsing the messages array, which
is exactly the full-body scan it was meant to avoid. So the path that carries
the 340 KB cost is precisely the path that cannot use byte-patching.

Where byte-patching *would* apply (text-only forward, no nested mutation), the
absolute saving is ~3 microseconds per request, negligible against multi-second
inference.

## Scope note

`encoding/json` always scans the **entire** body even when unmarshaling into a
2-field struct (it validates the whole document). So any approach that calls
`json.Unmarshal` on the full body pays the full-scan cost regardless of the
destination type. This is the floor, and it confirms the lever is "avoid
re-encoding the large payload," not "pick a faster map element type."

## JSON library comparison

The strategies above all use the stdlib `encoding/json`. A separate benchmark
compared four libraries on a workload that mirrors what the pipeline actually
does to a body: parse, read `model` and `stream`, add a `uuid` to each nested
image part, add the top-level `kv_transfer_params` and `tokens` keys, then
serialize for forwarding.

Libraries: stdlib `encoding/json`, `goccy/go-json`, `bytedance/sonic`,
`valyala/fastjson`. Same machine (Apple M4 Max, Go 1.25, `-benchmem`);
representative medians. fastjson uses a reused `Parser` + `Arena` (its intended
pattern).

### Text-only (~430 B)

| Library                 | ns/op | B/op | allocs/op | vs stdlib |
| ----------------------- | ----- | ---- | --------- | --------- |
| stdlib `encoding/json`  | 3727  | 3915 | 89        | 1.0x      |
| `goccy/go-json`         | 2451  | 3840 | 54        | 1.5x      |
| `bytedance/sonic`       | 2387  | 4773 | 29        | 1.6x      |
| `valyala/fastjson`      | 652   | 744  | 6         | 5.7x      |

### Multimodal (~340 KB base64 image)

| Library                 | ns/op     | B/op    | allocs/op | vs stdlib |
| ----------------------- | --------- | ------- | --------- | --------- |
| stdlib `encoding/json`  | 1,150,500 | 732 KB  | 105       | 1.0x      |
| `goccy/go-json`         | 309,589   | 737 KB  | 73        | 3.7x      |
| `bytedance/sonic`       | 80,880    | 964 KB  | 37        | 14x       |
| `valyala/fastjson`      | 41,663    | 353 KB  | 7         | 27x       |

### Why fastjson wins

fastjson is a **lazy** parser: it does not decode the 340 KB base64 string into
a Go value, it holds a pointer into the input and materializes only the fields
you touch. Reading two scalars, mutating one nested `uuid`, and adding two
top-level keys never copies the image through a `map[string]any`. This is the
same "do not round-trip the payload" lever the byte-patching experiment aimed
at, but delivered by the library, and unlike byte-splicing it handles the nested
`uuid` mutation cleanly. sonic is the runner-up (SIMD/JIT-assisted, mostly
amd64); go-json is a modest drop-in gain.

### Adoption cost

- `bytedance/sonic`: near drop-in `encoding/json` replacement (same
  `Marshal`/`Unmarshal` signatures). Import swap, minimal code change.
  amd64-optimized with an arm64 fallback.
- `goccy/go-json`: also drop-in, smaller gain.
- `valyala/fastjson`: **not** `Unmarshal`-compatible. Adopting it means
  rewriting every step that touches `RequestContext.Body` against
  `*fastjson.Value`. A `Parser` is not safe for concurrent use (needs a
  per-goroutine parser or a `ParserPool`), and parsed values are only valid
  until the next `Parse` on that parser. The encode fan-out is concurrent, so
  this needs care.
- Any of them is a new dependency and requires the usual sign-off and
  community review.

## Recommendation

Leave the body representation as `map[string]any` and the parser as stdlib
`encoding/json` unless profiling proves the router is CPU-bound on JSON under
multimodal load. Verify first via the per-step timing already emitted by the
pipeline at the default log level; the likely outcome is that prefill, decode,
and image download dominate, in which case no parsing change is worthwhile.

If a CPU/throughput bottleneck is confirmed, the pragmatic order is:

1. **`bytedance/sonic`** for a large win (14x multimodal) at low risk, mostly an
   import swap.
2. **`valyala/fastjson`** only if the higher ceiling (27x, 105 to 7 allocs) is
   needed and the cost of rewriting the body-handling steps against its API,
   plus the concurrency/lifetime constraints, is accepted.

Note these are throughput and GC-pressure wins (fewer allocations per request),
not user-visible latency: ~1ms of saved CPU is negligible against multi-second
GPU inference.

If multimodal throughput becomes the real constraint, the largest lever remains
architectural: avoid inlining base64 images into the JSON body and pass media
out-of-band / by reference. That removes the large-payload encode entirely,
which no library or map-type change can do.
