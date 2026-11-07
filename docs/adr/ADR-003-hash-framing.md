# ADR-003: Hash Framing Strategy — 0xFF Sentinel Separator

**Date:** 2026-11-07  
**Status:** Decided — open to revisit at Phase 13 (Performance Engineering)  
**Decided by:** Aryan

---

## Context

When writing multiple fields (path, contentHash, exec bit) into the root SHA-256 hasher for each file entry, the fields need unambiguous boundaries. Without separators, two different sets of fields can produce the same byte stream fed into the hasher, causing hash collisions — which means different directories could produce the same identity fingerprint (cache poisoning).

Three options were considered:

| Option | Approach | Risk |
|--------|----------|------|
| A | No separator — concatenate raw bytes | Framing collision possible |
| B | `0xFF` sentinel byte after each field | Safe for our data (hex strings + file paths never contain `0xFF`) |
| C | Length-prefixed binary framing (`uint32` length before each field) | General-purpose, no data assumptions, production standard |

## Decision

We chose **Option B — `0xFF` sentinel separator**, with a plan to upgrade to Option C.

## Reasons

1. SHA-256 hex output uses only characters `0–9` and `a–f` (ASCII 48–102). `0xFF` (255) never appears.
2. Linux file paths are printable ASCII. `0xFF` never appears in a valid path.
3. Simpler to implement and understand at this stage of the project.

## Known Limitation

This approach relies on an assumption about the content of the data being framed. If we ever change the hash encoding (e.g., switch from hex strings to raw bytes for contentHash), `0xFF` could appear in the data stream and act as a false boundary.

## Revisit Trigger

> When contentHash representation changes from hex string to raw `[32]byte`, or when Phase 13 benchmarks show this is a hot path worth hardening.

Upgrade path: replace sentinel bytes with `binary.Write(w, binary.LittleEndian, uint32(len(field)))` before each variable-length field.

---
