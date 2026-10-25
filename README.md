# Linkforge

A URL shortener with real-time click analytics, built incrementally as the course project for **Golang for developers**.

The design is specified in the project TDD; this repository implements the first two homework assignments on top of the course skeleton.

## Current state

Homework 2 implements `link.Service`, the `Shortener` backed by an in-memory store. It creates links with generated base62 codes or custom aliases, validates requests, checks expiration when resolving links, lists an owner's links with cursor pagination, and restricts deletion to the owner. The remaining course milestones retain their skeleton implementations.

Run the tests with `go test ./...`. Try the demo with `go run ./cmd/linkforge https://example.com ftp://nope https://go.dev`; invalid addresses are reported on stderr, and valid links receive consecutive IDs.

The skeleton interfaces and their no-op implementations remain in place. Types shared by several interfaces live in the package's `model.go`.

## Layout

```
cmd/      linkforged        service binary — prints its version, nothing more
          linkctl           operator CLI   — prints its version, nothing more
          linkforge         URL-shortening demo backed by an in-memory store

pkg/                        reusable libraries — a separate Go module
  shortid/  Codec           base62 encode/decode                       M1
            Generator       collision-resistant code issuing           M4
  cache/    Cache[K,V]      bounded cache on the redirect hot path     M4

internal/                   application code
  link/     Store           link persistence, declared by its consumer M1 / M7
            Shortener       the domain service behind every transport  M2
  store/                    in-memory link persistence                 M1 / M2
  base62/                   base62 encoding and decoding               M1 / M2
  click/    Ingester        non-blocking submission from the hot path  M5
            Store           append-only raw event persistence          M7
            Aggregator      time series and top-N queries              M5
            Publisher       fan-out to live WebSocket subscribers      M9
  auth/     Issuer          access token minting                       M9
            Verifier        token validation for middleware            M9
            UserStore       account persistence                        M7
  transport/Server          uniform listener lifecycle                 M6 / M8
  config/   Loader          environment-only configuration             M3
  observability/Metrics     counters and histograms                    M10
```

The `pkg/` tree is a second module so that reusable libraries stay free of any dependency on application code; `go.work` ties the two together for local development.

## Design rules

These hold for every milestone.

1. **Interfaces are declared by the consumer.** `link.Store` lives in `internal/link` because the shortener is what needs it — not beside any implementation. Constructors return concrete types.
2. **The dependency arrow points inwards.** `internal/link` and `internal/click` may not import a transport or a storage package.
3. **Analytics never blocks a redirect.** `Ingester.Submit` drops events under saturation rather than applying backpressure.
4. **Every listener shares one lifecycle contract**, which is what makes a single coordinated graceful shutdown possible.

## Working on it

```bash
make            # vet, lint and test — the same gates CI runs
make build
make test
make cover
make bench
make tidy
```

Requires Go 1.25+. Linting uses [golangci-lint](https://golangci-lint.run) (`brew install golangci-lint`).

## Milestones

| # | Lessons | Scope |
|---|---|---|
| M0 | 1 | Toolchain, module layout, CI |
| M1 | 2–3 | base62 codec, `Link` model, in-memory store |
| M2 | 4–5 | Methods, interfaces, generics, error values |
| M3 | 6–7 | Files and `context`, CLI, `slog`, tests |
| M4 | 8–9 | Own hash table, LRU, trie, top-N heap |
| M5 | 10–11 | Ingestion pipeline, worker pool, synchronisation |
| M6 | 12–13 | TCP resolver, graceful shutdown, Docker |
| M7 | 14 | PostgreSQL and MongoDB stores |
| M8 | 15–16 | `net/http` first, then a framework; REST and Swagger |
| M9 | 17–19 | Validation, JWT and middleware, WebSockets |
| M10 | 20–21 | Production hardening and project defence |
