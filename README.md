# 🎬 BookingCinema — Go Concurrent Handler with Redis

A mini real-time cinema booking app built to solve the **double-booking race condition** in a high-contention environment using Redis atomic operations and Go concurrency.

<p align="left">
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white" alt="Go"></a>
  <a href="https://go.dev/doc/effective_go#concurrency"><img src="https://img.shields.io/badge/Goroutines-Concurrency-00ADD8?logo=go&logoColor=white" alt="Goroutines"></a>
  <a href="https://redis.io/"><img src="https://img.shields.io/badge/Redis-7-DC382D?logo=redis&logoColor=white" alt="Redis"></a>
  <a href="https://github.com/redis/go-redis"><img src="https://img.shields.io/badge/go--redis-v9-DC382D" alt="go-redis"></a>
  <a href="https://www.docker.com/"><img src="https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white" alt="Docker"></a>
</p>

> ⚠️ Built for practice purposes — learning common backend concurrent cases.

## Table of Contents

- [The Case](#the-case)
- [Flow](#flow)
- [Tech Stack](#tech-stack)
- [How to Run](#how-to-run)
- [Concurrent Testing](#concurrent-testing)
- [Unit Testing](#unit-testing)

## The Case

Two users click "Book" on seat A1 at the same instant. Only one should win.

```text
User A ──► read seat A1 → "free" ──► write booking ──► success
User B ──► read seat A1 → "free" ──► write booking ──► ???
```

Without protection, both succeed — and two people show up for the same seat. The app handles this with **Redis `SET key value NX`**, an atomic check-and-set that guarantees only one writer wins, no matter how many concurrent requests hit it.

## Flow

```text
POST /hold ──► SET seat:{movie}:{seat} NX (TTL 2m) ──► ok? seat HELD : seat taken (409)
                                                                │
                                        ┌───────────────────────┴──────────────────┐
                              PUT /confirm                                DELETE /release
                        PERSIST key (no TTL)                                   DEL key
                           CONFIRMED                                          seat freed
```

1. **Hold** — a booking session is created via `SET ... NX` with a 2-minute TTL and a random UUID session ID. Atomicity here is the concurrency guard: concurrent holds for the same seat are serialized by Redis, exactly one succeeds.
2. **Confirm** — the session is made permanent: TTL removed (`PERSIST`), status flipped to `confirmed`.
3. **Release** — the hold is deleted and the seat becomes available again.
4. **Auto-expiry** — if the user never confirms, Redis TTL expires the hold and the seat frees itself — no cleanup cron needed.

Redis key design:

```text
seat:{movieID}:{seatID}    → session JSON (TTL = held, no TTL = confirmed)
session:{sessionID}        → seat key (reverse lookup)
```

## Tech Stack

| Tech | Role |
|---|---|
| **Go** | HTTP server, clean `handler → service → store` layering |
| **Goroutines** | Concurrent request handling & the 100k-goroutine load test |
| **Redis** | Atomic seat holds (`SET NX`), TTL-based sessions, the single source of truth |
| **go-redis v9** | Redis client (`SetArgs NX`, `Persist`, `Scan` iterator) |
| **Docker Compose** | One-command Redis + Redis Commander setup |
| **Redis Commander** | Web UI to inspect live keys on `:8081` |

## How to Run

```bash
# 1. Clone
git clone https://github.com/zannunakiz/cinema-book-concurrent.git
cd cinema-book-concurrent

# 2. Start Redis + Redis Commander
docker compose up -d

# 3. Run the server (listens on :8080)
go run ./cmd
```

| Endpoint | Description |
|---|---|
| `GET /movies` | List movies |
| `GET /movies/{movieID}/seats` | Seat map (booked / confirmed) |
| `POST /movies/{movieID}/seats/{seatID}/hold` | Hold a seat (2-min TTL) |
| `PUT /sessions/{sessionID}/confirm` | Confirm the booking |
| `DELETE /sessions/{sessionID}` | Release the hold |

Browse keys live at [localhost:8081](http://localhost:8081) (Redis Commander).

## Concurrent Testing

```bash
go test ./internal/booking/ -run '^TestConcurrentBooking' -v
```

> ⚠️ Requires a live Redis — run `docker compose up -d` first.

`service_test.go` runs `TestConcurrentBooking_ExactlyOneWins`: **100,000 goroutines** all race to book the same seat simultaneously, counted with `atomic.Int64` and synchronized via `WaitGroup`. The test asserts exactly **1 success** and **99,999 rejections** — proving `SET NX` eliminates the double-booking race even under extreme contention.

## Unit Testing

Mock-based unit tests validating the codebase logic — service delegation, HTTP handlers, store helpers, and in-memory concurrency — **without needing Redis**.

```bash
go test ./... -run '^TestUnit_' -v
```

| File | What it covers |
|---|---|
| `mock_test.go` | Hand-rolled `mockStore` test double for the `BookingStore` interface |
| `service_mock_test.go` | Service delegation & error propagation, `sessionKey` / `parseSession` helpers, `MemoryStore` / `ConcurrentStore` logic |
| `handler_mock_test.go` | HTTP handlers end-to-end via `httptest` (hold, list seats, confirm, release) |

These are also the tests that run in CI — see [`.github/workflows/unit-tests.yml`](.github/workflows/unit-tests.yml).

---

Built by **Richky Abednego** for learning common backend concurrent cases.
