# ADR-001: HashFile Return Type — string (hex) vs [32]byte

**Date:** 2026-11-07  
**Status:** Decided — open to revisit at Phase 13 (Performance Engineering)  
**Decided by:** Aryan

---

## Context

`HashFile` in `internal/hash/hasher.go` computes a SHA-256 digest of a file.  
SHA-256 always produces exactly 32 bytes. We had two choices for the return type:

| Option | Type | Notes |
|--------|------|-------|
| A | `[32]byte` | Fixed-size array, lives on stack, zero heap allocation, needs manual hex encoding when logging |
| B | `string` (hex-encoded) | 64-char hex string, human-readable, easy to compare with `==`, small heap allocation per call |

## Decision

We chose **`string` (hex-encoded)**.

## Reasons

1. At this stage (Phase 1), correctness and readability matter more than micro-optimisation.
2. Digests are frequently logged, printed, stored as keys, and compared. `string` does all of this naturally.
3. `[32]byte` comparisons with `==` work, but passing them around requires care (arrays are value-copied in Go, not reference-passed like slices).

## Known Trade-off

Each call to `HashFile` allocates a 64-byte string on the heap.  
Under high concurrency (Phase 13 load tests), this may appear in `pprof` as allocation pressure.  
If benchmarks show it is a measurable bottleneck, we switch the internal representation to `[32]byte` and expose a `String()` method for display.

## Revisit trigger

> If `go tool pprof` shows `hex.EncodeToString` allocations in the top 5 hot paths during load testing.

---
