# ADR-002: Module Import Path — github.com/aryansrivastava/venn

**Date:** 2026-11-07  
**Status:** Decided  
**Decided by:** Aryan

---

## Context

Go modules require a globally unique module path declared in `go.mod`.  
Two options:

| Option | Path | Notes |
|--------|------|-------|
| A | `venn` | Short, local-only, breaks if ever published to GitHub |
| B | `github.com/aryansrivastava/venn` | Matches the future GitHub remote, globally unique |

## Decision

We chose **`github.com/aryansrivastava/venn`**.

## Reason

Go's module system is decentralised. The module path doubles as the canonical download URL.  
Using a URL-style path means every internal import path is unambiguous and future-proof.  
Changing the module path after a project grows requires renaming every import statement in every file — painful.

## Consequence

Every internal import must use the full prefix:
```go
import "github.com/aryansrivastava/venn/internal/hash"
```

Short form like `"venn/internal/hash"` will cause a compile error because Go cannot resolve it.

---
