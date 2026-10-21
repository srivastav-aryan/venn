# Filesystem Fingerprint Machine + Content-Addressed Cache — Complete Notes

> These notes collect the important concepts we worked through, in order: first the filesystem fingerprint machine, then hashing/serialization, then filesystem internals, symlinks and cache poisoning, and finally the content-addressed cache, temp files, rename, mounts, buffering, `fsync`, and durability.

---

# 1. The original goal: build a filesystem fingerprint machine

The basic problem:

Given a directory, produce **one deterministic SHA-256 fingerprint that represents the relevant state of that directory**.

The important idea is that the fingerprint should depend on things that matter to the program:

- which files exist
- where they exist relative to the root
- what kind of filesystem object they are
- file contents
- executable permission state
- potentially symlink target information

The machine is essentially:

```text
directory
   ↓
walk filesystem tree
   ↓
inspect each relevant entry
   ↓
create one entry record per item
   ↓
hash file contents / symlink information
   ↓
sort entries deterministically
   ↓
serialize every entry into bytes
   ↓
feed all bytes into one master SHA-256
   ↓
32-byte fingerprint
   ↓
64-character hexadecimal string
```

The important mental model:

> The filesystem is the input. The final SHA-256 is a compact representation of the filesystem state according to the rules we chose.

---

# 2. The fingerprint record

We used a structure like:

```go
type entry struct {
    path string
    exec bool
    hash [32]byte
}
```

Later, when distinguishing filesystem kinds became important, the conceptual record becomes:

```go
type entry struct {
    path string
    kind byte
    exec bool
    hash [32]byte
}
```

For example:

```text
path = "go.mod"
kind = 'f'
exec = false
hash = SHA256(contents of go.mod)
```

or:

```text
path = ".nvmrc"
kind = 'l'
exec = false
hash = SHA256(the symlink target string)
```

The exact fields are a design choice, but the principle is:

> Every filesystem object that matters to the fingerprint must have a deterministic byte representation.

---

# 3. `entries` is a slice of structs

If we have:

```go
var entries []entry
```

this means:

- `entries` is a **slice**
- each element is an `entry` struct

For example:

```text
entries
[
    {
        path: "go.mod",
        exec: false,
        hash: H1
    },
    {
        path: "main.go",
        exec: false,
        hash: H2
    }
]
```

The slice is not the final fingerprint.

It is an intermediate collection of information that we later sort and serialize.

---

# 4. Walking the directory

The basic walker was:

```go
filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
    ...
})
```

Important values:

```go
path string
d    fs.DirEntry
err  error
```

`path` is the current path being visited.

`d` describes the directory entry.

`err` reports an error encountered while trying to access the entry.

The callback returns an `error` so it can tell `WalkDir`:

- continue with `nil`
- stop with an actual error
- skip a directory with `filepath.SkipDir`

---

# 5. `fs` is `io/fs`

With:

```go
import "io/fs"
```

the package name is `fs`.

So:

```go
fs.DirEntry
fs.FileMode
fs.FileInfo
```

are types from Go's `io/fs` package.

---

# 6. Regular files

A regular file is an ordinary file containing bytes.

Examples:

```text
main.go
go.mod
README.md
package.json
```

A basic test is:

```go
if !d.Type().IsRegular() {
    return nil
}
```

This means:

> If this directory entry is not a regular file, ignore it for this particular logic.

But this became insufficient once symlinks mattered.

---

# 7. Why symlinks broke the simple model

Suppose a trusted project contains:

```text
.nvmrc
```

as a regular file:

```text
.nvmrc
    contents = "18"
```

Now imagine another project has:

```text
.nvmrc -> 18
```

where `18` is a symlink target.

These are **not the same filesystem object**.

One is:

```text
regular file
```

The other is:

```text
symbolic link
```

But if the fingerprinting system follows the symlink and only hashes the resulting bytes, both could potentially produce the same content hash.

That is dangerous for a cache.

---

# 8. Cache poisoning caused by treating different objects as identical

Suppose:

```text
trusted project:

.nvmrc
  regular file
  contents: 18
```

and:

```text
attacker-controlled project:

.nvmrc
  symlink
  target: 18
```

If the fingerprint says only:

```text
.nvmrc + SHA256("18")
```

then both states may receive the same fingerprint.

That means:

```text
different filesystem state
        ↓
same fingerprint
        ↓
same cache key
```

Now a cached result produced for one state may be reused for another state.

That is the core cache-poisoning / semantic-collision problem.

---

# 9. The critical lesson: filesystem semantics matter

A hash function only sees bytes.

It does not know:

```text
"this came from a regular file"
```

or:

```text
"this came from a symlink"
```

unless we encode that distinction ourselves.

Therefore:

> A filesystem fingerprint is not automatically correct just because SHA-256 is strong.

The correctness comes from the **serialization format and filesystem semantics we choose before hashing**.

---

# 10. Three possible symlink policies

When encountering a symlink, there are several possible designs.

## Policy A: hash the symlink itself

Read the symlink target:

```go
target, err := os.Readlink(path)
```

For:

```text
.nvmrc -> 18
```

the target string is:

```text
"18"
```

Then hash:

```text
SHA256("18")
```

while also recording:

```text
kind = 'l'
```

So a symlink and regular file remain distinguishable.

---

## Policy B: follow the symlink

Use the target's contents.

For:

```text
.nvmrc -> some/file
```

hash the contents of `some/file`.

This can be useful, but it introduces extra semantics:

- dangling symlinks
- symlink cycles
- targets outside the tree
- target changes affecting the fingerprint
- whether external files are allowed to influence the project fingerprint

So this policy must be deliberate.

---

## Policy C: reject symlinks

The fingerprinting system can simply say:

```text
symlink found → error
```

This is often easier to reason about if symlinks are not part of the supported input model.

---

# 11. Symlink cycles

Symlinks can form cycles.

For example:

```text
a -> b
b -> a
```

If the implementation blindly follows symlinks, it can repeatedly chase:

```text
a
 ↓
b
 ↓
a
 ↓
b
 ↓
...
```

Therefore:

> "Follow symlinks" is not a trivial rule. It creates traversal semantics that must be handled explicitly.

---

# 12. Dangling symlinks

A symlink can exist even when its target does not.

Example:

```text
latest -> release-v99
```

where:

```text
release-v99
```

does not exist.

The symlink itself is still a real filesystem object.

This is another reason why:

```text
"does the target exist?"
```

and:

```text
"does the symlink exist?"
```

are different questions.

---

# 13. Symlinks pointing outside the project

Consider:

```text
project/
    config -> /some/external/location/config
```

If the fingerprint follows the symlink, the project fingerprint now depends on something outside:

```text
project/
```

That can be surprising and dangerous.

Changing the external file could change the project's fingerprint.

So a fingerprint design should explicitly decide whether external symlink targets are allowed.

---

# 14. `Stat` versus `Lstat`

This became important when distinguishing symlinks.

## `os.Stat`

```go
info, err := os.Stat(path)
```

`Stat` follows symlinks.

If:

```text
link -> target
```

then `os.Stat(link)` generally reports information about:

```text
target
```

## `os.Lstat`

```go
info, err := os.Lstat(path)
```

`Lstat` reports information about the symlink itself.

So:

```text
Stat  → follow link
Lstat → inspect link
```

This distinction is fundamental when a fingerprint must preserve the difference between:

```text
regular file
```

and:

```text
symbolic link
```

---

# 15. `DirEntry.Type()`

`d.Type()` returns a `fs.FileMode`.

For example:

```go
d.Type()
```

can contain type information such as:

```text
directory
symlink
regular file
```

A symlink check can look like:

```go
if d.Type()&fs.ModeSymlink != 0 {
    ...
}
```

The `&` here is bitwise AND.

It tests whether the symlink type bit is present.

---

# 16. `d.Type().IsRegular()`

This is the simple regular-file test:

```go
d.Type().IsRegular()
```

It is preferable to confusing constructions such as trying to turn `Type()` into another mode object.

---

# 17. The important control-flow bug: double processing

Suppose the code does:

```go
if symlink {
    appendSymlink()
}

if regular {
    appendRegular()
}
```

If the conditions overlap or the logic is poorly structured, one filesystem object can accidentally be processed twice.

The safe conceptual structure is:

```go
if symlink {
    // handle symlink
    return nil
}

if regular {
    // handle regular file
    return nil
}

return nil
```

Or use an explicit `if / else if / else` structure.

The important point:

> Once an object has been classified and handled, control flow must not accidentally fall through into another classification.

---

# 18. Executable bits

We used:

```go
info.Mode().Perm()&0o111 != 0
```

`0o111` means:

```text
owner execute   = 0o100
group execute   = 0o010
other execute   = 0o001
```

Together:

```text
0o111
```

So:

```go
info.Mode().Perm() & 0o111
```

checks whether **any executable permission bit** is set.

The comparison:

```go
!= 0
```

turns that into a boolean.

---

# 19. `filepath.Rel`

Given:

```text
root = /project
path = /project/src/main.go
```

then:

```go
filepath.Rel(root, path)
```

produces:

```text
src/main.go
```

This is important because the fingerprint should normally contain the file's path **relative to the fingerprint root**, not an absolute machine-specific path.

Otherwise:

```text
/home/alice/project
```

and:

```text
/home/bob/project
```

could produce different fingerprints even when their contents are identical.

---

# 20. `filepath.ToSlash`

Different operating systems use different path separators.

For example:

```text
Unix:    src/main.go
Windows: src\main.go
```

Using:

```go
filepath.ToSlash(rel)
```

normalizes the path representation to:

```text
src/main.go
```

This helps make fingerprints portable across operating systems when that is desired.

---

# 21. Hashing a regular file

The file hashing function was conceptually:

```go
func hashFile(p string) ([32]byte, error) {
    var out [32]byte

    f, err := os.Open(p)
    if err != nil {
        return out, err
    }
    defer f.Close()

    h := sha256.New()

    if _, err := io.Copy(h, f); err != nil {
        return out, err
    }

    copy(out[:], h.Sum(nil))
    return out, nil
}
```

The flow:

```text
path
 ↓
os.Open
 ↓
*os.File
 ↓
sha256.New
 ↓
io.Copy(file → hasher)
 ↓
h.Sum(nil)
 ↓
32 bytes
```

---

# 22. Why `io.Copy(h, f)` works

`io.Copy` takes:

```go
io.Copy(dst, src)
```

The file:

```go
f
```

is the source.

The hash object:

```go
h
```

is the destination.

The hash object implements `io.Writer`.

So:

```go
io.Copy(h, f)
```

means approximately:

```text
read some file bytes
    ↓
write them into hash
    ↓
read more
    ↓
write more
    ↓
...
```

The entire file does not have to be loaded into one giant byte slice.

That is why streaming hashing is suitable for large files.

---

# 23. `sha256.New()` versus `sha256.Sum256`

Two useful forms:

## Streaming

```go
h := sha256.New()
io.Copy(h, f)
sum := h.Sum(nil)
```

Useful for files or streams.

## One-shot

```go
sum := sha256.Sum256(data)
```

Useful when all data is already in memory.

The return type of:

```go
sha256.Sum256(data)
```

is:

```go
[32]byte
```

---

# 24. Why SHA-256 is 32 bytes

SHA-256 produces:

```text
256 bits
```

There are:

```text
8 bits per byte
```

Therefore:

```text
256 / 8 = 32 bytes
```

So:

```go
[32]byte
```

is exactly large enough to hold a SHA-256 digest.

---

# 25. Array versus slice

These are different Go types:

```go
[32]byte
```

and:

```go
[]byte
```

An array has a fixed length.

A slice is a descriptor referring to an underlying array.

Therefore:

```go
sum := sha256.Sum256(data)
```

gives:

```go
[32]byte
```

while:

```go
master.Write(...)
```

expects:

```go
[]byte
```

So we use:

```go
sum[:]
```

to create a slice covering the array's 32 bytes.

---

# 26. `e.hash[:]`

If:

```go
e.hash
```

has type:

```go
[32]byte
```

then:

```go
e.hash[:]
```

has type:

```go
[]byte
```

and refers to all 32 bytes.

It does **not** hash:

```text
pointer
length
capacity
```

of the slice.

Those are slice metadata used by Go to know where/how many bytes to access.

The actual bytes passed to `Write` are:

```text
byte 0
byte 1
...
byte 31
```

---

# 27. Hex encoding is not hashing

Suppose SHA-256 returns 32 bytes.

Those bytes can be displayed as:

```text
2c f2 4d ba ...
```

Hex encoding converts each byte into two hexadecimal characters.

Therefore:

```text
32 bytes × 2 = 64 hex characters
```

This:

```go
hex.EncodeToString(sum[:])
```

does **not** calculate another hash.

It only creates a human/filesystem-friendly textual representation.

---

# 28. The master hash

After creating one entry per file, we sort:

```go
sort.Slice(entries, func(i, j int) bool {
    return entries[i].path < entries[j].path
})
```

Then create:

```go
master := sha256.New()
```

The master hash is one running SHA-256 state.

For every entry:

```go
master.Write(...)
master.Write(...)
master.Write(...)
```

all of those bytes go into the **same** hash stream.

There is not a separate master hash for each entry.

---

# 29. Why sorting is necessary

Filesystem traversal order should not be treated as the semantic identity of a directory.

Suppose the same files are discovered in two orders:

```text
main.go
go.mod
README.md
```

versus:

```text
README.md
go.mod
main.go
```

If the master hash receives those records in different orders, it receives different byte streams and therefore produces different hashes.

Sorting gives deterministic serialization.

So:

```text
same filesystem state
      ↓
same sorted entries
      ↓
same byte stream
      ↓
same fingerprint
```

---

# 30. Serialization

Suppose:

```text
entry 1:
path = "go.mod"
exec = false
hash = H1

entry 2:
path = "main.go"
exec = false
hash = H2
```

The master stream can conceptually be:

```text
"go.mod"
0-byte
0-byte
H1

"main.go"
0-byte
0-byte
H2
```

More explicitly:

```text
go.mod \0 \0 [32 hash bytes]
main.go \0 \0 [32 hash bytes]
```

The hash function does not know that these are fields.

We are defining the structure by the bytes we feed it.

---

# 31. Why separators exist

SHA-256 sees:

```text
a long sequence of bytes
```

It does not know where:

```text
path
```

ends and:

```text
exec
```

begins.

Separators or fixed-width fields make the serialization unambiguous and deterministic.

Important nuance:

> In our exact encoding, the executable flag plus fixed 32-byte hash already gives substantial structure, so removing a particular separator does not automatically mean the program becomes unsafe. The separator is part of the serialization format and helps make boundaries explicit.

---

# 32. A better conceptual serialization format

A robust conceptual record might be:

```text
kind
path
separator
exec flag
content/target hash
```

For example:

```text
f go.mod \0 0 H1
f main.go \0 0 H2
l .nvmrc \0 0 H3
```

The crucial part is that a regular file and symlink have different `kind` values.

For example:

```text
'f' = regular file
'l' = symbolic link
```

Then:

```text
regular .nvmrc containing "18"
```

and:

```text
symlink .nvmrc -> "18"
```

cannot accidentally serialize as the same record merely because their underlying bytes happen to match.

---

# 33. The final fingerprint

After all entries are serialized:

```go
sum := master.Sum(nil)
```

returns the final digest bytes.

Then:

```go
fmt.Printf("%x\n", sum)
```

prints those bytes as hexadecimal.

A SHA-256 fingerprint is therefore:

```text
32 raw bytes
```

or:

```text
64 hex characters
```

depending on representation.

---

# 34. The complete fingerprint machine

The full mental model is:

```text
PROJECT DIRECTORY
      │
      ▼
WalkDir
      │
      ├── regular file
      │      │
      │      ├── relative path
      │      ├── executable bit
      │      └── SHA256(file contents)
      │
      ├── symlink
      │      │
      │      ├── relative path
      │      ├── kind = symlink
      │      └── SHA256(target string)
      │
      └── ignored/unsupported object
             │
             ▼
        []entry
             │
             ▼
        sort by path
             │
             ▼
        serialize entries
             │
             ▼
        master SHA-256
             │
             ▼
       32-byte fingerprint
             │
             ▼
       64-char hex string
```

---

# 35. Filesystem objects: names versus objects

A critical OS/filesystem concept:

A filename is not the file itself.

Conceptually, a directory contains mappings like:

```text
"name" → filesystem object
```

For example:

```text
main.go → inode/object
```

The filesystem object stores metadata and references to file data.

A simplified model:

```text
directory
    │
    ├── "main.go" ──────► inode/object
    │                         │
    │                         ├── metadata
    │                         └── data blocks
    │
    └── "go.mod" ───────► inode/object
                              │
                              ├── metadata
                              └── data blocks
```

This is simplified; exact filesystem structures differ.

---

# 36. A directory is itself a filesystem object

A directory is not just an abstract Go map.

Conceptually it is a filesystem object whose contents represent name-to-object relationships.

For example:

```text
project/
```

might contain entries:

```text
"main.go" → object A
"go.mod"  → object B
"src"     → object C
```

where `src` itself is a directory object.

---

# 37. `os.Open`

When Go does:

```go
f, err := os.Open("main.go")
```

the high-level flow is roughly:

```text
Go program
    ↓
os.Open
    ↓
Go's OS-specific implementation
    ↓
runtime / syscall / OS API
    ↓
kernel
    ↓
filesystem
    ↓
file descriptor / handle
    ↓
*os.File
```

Do not interpret this as meaning every operating system uses exactly one identical syscall.

The exact lower-level implementation varies by OS and Go version.

---

# 38. `*os.File`

`os.Open` returns:

```go
(*os.File, error)
```

The `*os.File` is a Go object representing an open file.

On Unix-like systems, it is associated with an OS file descriptor.

On Windows, the underlying OS mechanism is different.

So:

```text
*os.File
```

is Go's portable abstraction around the OS-level open file resource.

---

# 39. `os.Stat`

```go
info, err := os.Stat(path)
```

returns metadata.

It does not mean:

```text
read entire file into memory
```

It is about properties such as:

```text
name
size
permissions
modification time
type
system-specific metadata
```

The returned type is:

```go
fs.FileInfo
```

which is an interface.

---

# 40. `fs.FileInfo` versus concrete implementation

`fs.FileInfo` is an interface.

A Unix implementation may use an internal concrete structure roughly like:

```go
type fileStat struct {
    name    string
    size    int64
    mode    FileMode
    modTime time.Time
    sys     syscall.Stat_t
}
```

The exact implementation is internal and platform-dependent.

`syscall.Stat_t` corresponds to the operating system's stat metadata structure.

Do not think of it as literally embedding a C structure unchanged inside Go.

---

# 41. `d.Info()`

`d.Info()`:

```go
info, err := d.Info()
```

returns:

```go
(fs.FileInfo, error)
```

It gives metadata about the directory entry.

It is not correct to assume it is literally implemented as:

```go
os.Stat(path)
```

on every platform.

The exact implementation can vary.

---

# 42. `d.Type()` versus `d.Info().Mode()`

There are two related concepts:

```go
d.Type()
```

and:

```go
info.Mode()
```

`d.Type()` provides type information already available from the directory entry.

`info.Mode()` gives the complete mode information from the file metadata.

For permission checks such as executable bits, using:

```go
info.Mode().Perm()
```

is appropriate.

---

# 43. `os.Remove`

```go
os.Remove(path)
```

removes a filesystem directory entry/object reference according to filesystem semantics.

It does not mean:

```text
overwrite every byte with zeroes
```

and it does not guarantee physical erasure of old storage blocks.

For a regular file, removing the name can make the file inaccessible through that directory entry.

If other hard links or open references exist, the underlying object can remain alive.

---

# 44. `os.RemoveAll`

```go
os.RemoveAll(path)
```

recursively removes a path and its contents.

This is fundamentally different from:

```go
os.Remove(path)
```

which does not recursively delete a non-empty directory.

---

# 45. The content-addressed cache

After building the fingerprint machine, we built a content-addressed store.

The basic idea:

```text
data
  ↓
SHA-256(data)
  ↓
hash
  ↓
use hash as identity/name
  ↓
store data under that hash
```

This means the object's identity is derived from its contents.

For:

```go
data := []byte("hello")
```

the SHA-256 is:

```text
2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```

---

# 46. Hash-to-path layout

Instead of putting every object into one enormous directory:

```text
store/
    object1
    object2
    object3
    ...
```

we split the hash.

For:

```text
2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```

use:

```text
first two hex chars = 2c
remaining chars     = f24dba...
```

so the path becomes:

```text
store/
└── 2c/
    └── f24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```

---

# 47. Why two hex characters?

Each hex character has 16 possibilities.

Two characters therefore give:

```text
16 × 16 = 256
```

possible directories.

So:

```text
00
01
02
...
ff
```

gives 256 buckets.

With uniformly distributed cryptographic hashes, objects are roughly distributed across these buckets.

This reduces the number of entries in any one directory.

It is a common filesystem scalability technique; it is not a universal guarantee that one giant directory is always slow.

---

# 48. `filepath.Join`

Given:

```go
dir := filepath.Join(storeDir, name[:2])
```

Go combines path components using the current operating system's path separator and cleans the resulting path.

For example:

```text
store + 2c
```

becomes:

```text
store/2c
```

on Unix-like systems.

---

# 49. `os.MkdirAll`

```go
os.MkdirAll(dir, 0o755)
```

means:

> Create this directory and any missing parent directories.

If they already exist, that is normally okay.

`0o755` means approximately:

```text
owner:  rwx
group:  r-x
others: r-x
```

A private cache may instead use something like:

```go
0o700
```

depending on requirements.

---

# 50. The basic `put` function

The cache writer became:

```go
func put(storeDir string, data []byte) (string, error) {
    sum := sha256.Sum256(data)
    name := hex.EncodeToString(sum[:])

    dir := filepath.Join(storeDir, name[:2])
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return "", err
    }

    final := filepath.Join(dir, name[2:])

    if _, err := os.Stat(final); err == nil {
        return name, nil
    }

    tmp, err := os.CreateTemp(dir, "tmp-*")
    if err != nil {
        return "", err
    }

    if _, err := tmp.Write(data); err != nil {
        tmp.Close()
        os.Remove(tmp.Name())
        return "", err
    }

    if err := tmp.Close(); err != nil {
        os.Remove(tmp.Name())
        return "", err
    }

    if err := os.Rename(tmp.Name(), final); err != nil {
        os.Remove(tmp.Name())
        return "", err
    }

    return name, nil
}
```

---

# 51. Exact example: storing `"hello"`

Run:

```bash
go run main.go hello
```

Then:

```go
os.Args[1]
```

is:

```text
"hello"
```

and:

```go
[]byte("hello")
```

is:

```text
[104 101 108 108 111]
```

because:

```text
h = 104
e = 101
l = 108
l = 108
o = 111
```

---

# 52. Hashing `"hello"`

```go
sum := sha256.Sum256([]byte("hello"))
```

produces the 32-byte SHA-256 digest corresponding to:

```text
2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```

Then:

```go
name := hex.EncodeToString(sum[:])
```

makes that into a Go string.

---

# 53. Splitting the hash

```go
name[:2]
```

gives:

```text
2c
```

and:

```go
name[2:]
```

gives:

```text
f24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
```

Therefore:

```go
dir = store/2c
final = store/2c/f24dba...
```

---

# 54. Why use a temporary file?

Do **not** directly do:

```text
open final path
write data
```

if you want the final cache path to mean:

> a complete valid object exists here.

Imagine the program writes:

```text
store/2c/f24d...
```

directly.

Suppose the process crashes halfway through.

Now the final path exists but contains:

```text
PARTIAL DATA
```

Later the cache checks:

```go
os.Stat(final)
```

and sees that the path exists.

It may incorrectly conclude:

```text
cache hit
```

even though the content is incomplete.

---

# 55. Temporary file + rename

Instead:

```text
create temporary file
        ↓
write complete contents
        ↓
close it
        ↓
rename temp → final
```

The final name is only published after the complete object has been built.

For example:

```text
store/2c/tmp-83746291
```

contains the data while it is being constructed.

Only later:

```text
store/2c/f24dba...
```

appears as the final cache object.

---

# 56. The three error-cleanup blocks

This code:

```go
if _, err := tmp.Write(data); err != nil {
    tmp.Close()
    os.Remove(tmp.Name())
    return "", err
}

if err := tmp.Close(); err != nil {
    os.Remove(tmp.Name())
    return "", err
}

if err := os.Rename(tmp.Name(), final); err != nil {
    os.Remove(tmp.Name())
    return "", err
}
```

does **not** execute all three cleanup blocks every time.

Each `os.Remove` belongs to a particular failure path.

Normal execution:

```text
Write succeeds
    ↓
Close succeeds
    ↓
Rename succeeds
    ↓
return name
```

No cleanup block runs.

---

# 57. Failure state machine

Think of the cache write as:

```text
CREATE TEMP
    │
    ▼
WRITE
    │
    ├── failure → close/remove temp → error
    │
    ▼
CLOSE
    │
    ├── failure → remove temp → error
    │
    ▼
RENAME
    │
    ├── failure → remove temp → error
    │
    ▼
DONE
```

This is the cleanest mental model for the code.

---

# 58. Why the temp file is in the destination directory

We use:

```go
os.CreateTemp(dir, "tmp-*")
```

instead of:

```go
os.CreateTemp("", "tmp-*")
```

because the destination directory is deliberately chosen.

The temp file then lives beside the final file:

```text
store/2c/
    tmp-1234
    f24dba...
```

This matters because `rename` is normally a same-filesystem operation.

---

# 59. Filesystems and mounts

A computer can have one unified directory tree while different directories belong to different filesystem instances.

Conceptually:

```text
/
├── home
│    └── filesystem A
│
└── mnt/data
     └── filesystem B
```

So:

```text
/home/project
```

and:

```text
/mnt/data/project
```

may be on different filesystems even though they are on the same computer.

"Same disk" and "same filesystem" are not necessarily the same thing.

---

# 60. Why rename is normally same-filesystem only

Within one filesystem, rename can often be implemented as a filesystem metadata operation:

```text
directory entry A
        ↓
new directory entry/name
        ↓
same underlying object
```

It does not need to copy the entire file's contents.

Across two separate filesystem instances, one filesystem cannot simply edit the other's internal directory tables as if they were one structure.

On Unix-like systems, a cross-filesystem rename commonly fails with:

```text
EXDEV
```

meaning the operation crosses a device/filesystem boundary.

The general lesson:

> A rename is not a general-purpose file copy.

---

# 61. Why temp-in-destination-directory matters

If the temporary file were created in:

```text
/tmp
```

while the final object belongs in:

```text
store/
```

then `/tmp` and `store/` might be different filesystems.

Then:

```text
/tmp/tmp-123
       ↓ rename
store/object
```

could fail.

Creating the temp file in:

```text
store/2c/
```

means the temp and final path are intended to live on the same filesystem.

---

# 62. `os.Rename`

The conceptual operation:

```go
os.Rename(tmp.Name(), final)
```

means:

```text
temporary name
      ↓
final name
```

It is not:

```text
read entire temp file
write entire final file
delete temp
```

when the operation can be performed as a same-filesystem rename.

This is why it is useful for publishing completed cache objects.

---

# 63. Atomic publication versus durability

These are two different properties.

## Rename / atomic publication

Answers:

> Can readers see a partially written final file?

Temp + rename is designed to prevent that.

## Durability

Answers:

> After a sudden crash or power loss, is the completed data guaranteed to still be there?

That is where `fsync`/`Sync` enters.

Do not confuse:

```text
atomicity
```

with:

```text
durability
```

---

# 64. What `Write` really means

This was an important correction to the mental model.

Calling:

```go
tmp.Write(data)
```

does not necessarily mean:

```text
every byte is physically stored in NAND/SSD right now
```

A simplified model is:

```text
Go program
    ↓
kernel / filesystem
    ↓
memory/page cache and/or device caches
    ↓
storage device
```

The OS may accept the write into its buffering/cache machinery before the storage device has physically persisted everything.

Exact behavior depends on:

- operating system
- filesystem
- storage device
- device cache
- filesystem mount/configuration
- synchronization operations

---

# 65. Why RAM matters

RAM is volatile.

If data exists only in volatile memory and power suddenly disappears, that data can be lost.

Persistent storage is designed to survive power loss.

So the system may have a situation conceptually like:

```text
program wrote data
       ↓
OS accepted it
       ↓
data is currently buffered/cached
       ↓
power failure
       ↓
data may not survive
```

This is why "the write call returned successfully" and "the data is definitely durable after power loss" are not automatically the same statement.

---

# 66. `fsync` / Go's `File.Sync`

In Go:

```go
tmp.Sync()
```

is the relevant file synchronization operation.

Conceptually it asks the OS/storage stack to push the file's relevant buffered state toward stable storage according to the guarantees available on that platform/filesystem/device.

The key idea:

```text
Write
    ↓
OS accepted bytes
    ↓
Sync
    ↓
request durability boundary
```

It is about **durability**, not about "rendering the file."

---

# 67. Safer durable cache publication sequence

For stronger durability requirements, the conceptual sequence is:

```text
write temp
    ↓
tmp.Sync()
    ↓
tmp.Close()
    ↓
os.Rename(temp, final)
```

The reasoning:

1. Build the entire temp file.
2. Ask for its contents to become durable.
3. Close it.
4. Publish the final name.

Then the final name is not published until the file contents have crossed the desired durability boundary.

---

# 68. Directory synchronization

For very strong crash-durability guarantees, the rename itself changes directory metadata.

Therefore a production design may also need to synchronize the directory after rename:

```text
write
  ↓
fsync file
  ↓
close
  ↓
rename
  ↓
fsync directory
```

Exact implementation and guarantees are platform/filesystem-dependent.

The important conceptual point:

> The file's contents and the directory entry that names the file are separate pieces of filesystem state.

---

# 69. Why a cache may skip `fsync`

A cache is often rebuildable.

If a machine loses power and the cache object disappears or becomes unavailable, the program may simply recompute it.

Therefore:

```text
strong durability
```

may not be worth the performance cost for every cache write.

A reasonable first cache implementation may use:

```text
write
close
rename
```

without explicit `Sync`.

A production system must decide its durability requirements deliberately.

---

# 70. Node.js has the same underlying issue

Node does not bypass the operating system's filesystem machinery.

Node APIs such as:

```text
fs.write
fs.writeFile
streams
file handles
```

ultimately interact with the OS/filesystem.

Therefore the same broad concepts exist:

```text
application
    ↓
Node
    ↓
OS/filesystem
    ↓
memory/page cache/device cache
    ↓
storage
```

Node also exposes `fsync`-style synchronization APIs, including file-handle synchronization methods and lower-level APIs depending on the API used.

The exact behavior still depends on the OS/filesystem/storage stack.

---

# 71. `go run` and `os.Args`

When running:

```bash
go run main.go
```

Go generally builds a temporary executable and runs it.

Therefore:

```go
fmt.Println(os.Args[0])
```

may print something resembling:

```text
/tmp/go-build.../exe/main
```

It is the executable path.

---

# 72. `os.Args[1]`

`os.Args` contains:

```text
os.Args[0] = executable
os.Args[1:] = program arguments
```

So:

```bash
go run main.go
```

does not mean:

```text
os.Args[1] == "main.go"
```

`main.go` is an argument to the **Go tool**, not necessarily an argument to your program.

If you run:

```bash
go run . main.go
```

then `main.go` is passed to your program as an argument.

So:

```text
os.Args[0] = temporary executable
os.Args[1] = "main.go"
```

---

# 73. `index out of range [1] with length 1`

If:

```go
os.Args[1]
```

panics with:

```text
index out of range [1] with length 1
```

the literal meaning is:

```text
requested index = 1
slice length = 1
```

A slice of length 1 has only:

```text
index 0
```

So index 1 does not exist.

This is why command-line programs should check argument counts before accessing:

```go
os.Args[1]
```

---

# 74. Important difference: fingerprint hash versus cache object hash

There are two related but distinct uses of hashing.

## Fingerprint

The fingerprint hashes a **serialized description of filesystem state**:

```text
path
kind
exec
file hash / symlink hash
...
```

It identifies a directory/project state.

## Content-addressed object hash

The cache object hash hashes the **actual stored bytes**:

```text
SHA256(data)
```

It identifies the data object itself.

So:

```text
project fingerprint
```

and:

```text
cache blob hash
```

are not automatically the same concept.

---

# 75. Cache lookup semantics

The cache layout lets us do:

```text
hash
 ↓
derive path
 ↓
check whether object exists
 ↓
if exists → cache hit
else       → compute/store
```

For a content-addressed cache:

```text
hash = identity
```

rather than:

```text
filename = identity
```

The filename is derived from the content hash.

---

# 76. Cache races

Two processes can attempt to store the same object at the same time:

```text
Process A                  Process B
    │                          │
    ├── check final ──────────►│
    │                          ├── check final
    │                          │
    ├── create temp            ├── create temp
    │                          │
    ├── write                  ├── write
    │                          │
    ├── rename                 ├── rename
```

Both may discover that the final path did not exist.

This is not automatically a corruption problem if publication is designed correctly.

The cache must decide what should happen when both processes race to publish the same content.

The exact behavior of `os.Rename` when the destination already exists varies by operating system and must be treated deliberately.

---

# 77. Cache correctness is larger than cryptographic collision resistance

SHA-256 collision resistance is only one piece.

A cache can still be wrong if the fingerprinting system maps semantically different states to the same byte representation.

For example:

```text
regular file
```

versus:

```text
symlink
```

could become equivalent if the serialization discards the distinction.

So there are two very different questions:

```text
Can someone find two arbitrary inputs with the same SHA-256?
```

versus:

```text
Did our program accidentally encode two different filesystem states as the same input?
```

The second problem is a **serialization/semantic correctness bug**, not a failure of SHA-256.

---

# 78. Determinism requirements

A good fingerprint must make the same filesystem state produce the same bytes.

Things that can break determinism include:

- traversal order
- platform-dependent path separators
- absolute paths
- unspecified treatment of symlinks
- inconsistent metadata encoding
- ambiguous serialization
- nondeterministic inclusion/exclusion rules

The general formula is:

```text
same semantic state
        ↓
same canonical representation
        ↓
same bytes
        ↓
same hash
```

---

# 79. The fingerprint machine is a canonicalization system

A useful way to think about the entire machine:

```text
filesystem state
      ↓
canonical representation
      ↓
cryptographic digest
```

The hard part is often not SHA-256.

The hard part is deciding:

> What exactly counts as the same filesystem state for this program?

Once that definition is correct and deterministic, SHA-256 is the final compression step.

---

# 80. The cache write machine

The content-addressed writer has a similar state machine:

```text
data
 ↓
SHA-256
 ↓
hash string
 ↓
hash-derived directory
 ↓
create directory
 ↓
check final
 ↓
create temp
 ↓
write complete object
 ↓
close
 ↓
rename temp → final
 ↓
object published
```

With stronger durability:

```text
write
 ↓
Sync
 ↓
close
 ↓
rename
 ↓
directory Sync if required
 ↓
durable publication
```

---

# 81. The most important distinctions

## Bytes versus text

```text
[]byte
```

contains bytes.

```text
string
```

contains text/bytes represented as a Go string.

Hex encoding turns:

```text
raw hash bytes
```

into:

```text
printable hexadecimal text
```

---

## Array versus slice

```text
[32]byte
```

is a fixed-size array.

```text
[]byte
```

is a slice view over bytes.

Use:

```go
sum[:]
```

when an API requires a byte slice.

---

## File content versus filename

```text
filename
```

is a directory name/entry.

```text
file object
```

contains metadata and references to data.

---

## `Stat` versus `Lstat`

```text
Stat  → follows symlink
Lstat → examines symlink itself
```

---

## Rename versus copy

```text
rename
```

is primarily a name/directory-entry operation within a filesystem.

```text
copy
```

means reading bytes and writing them somewhere else.

---

## Atomicity versus durability

```text
rename
```

helps publish the complete object without exposing a partially written final file.

```text
fsync / Sync
```

is about persistence across crashes/power loss.

---

## Cache identity versus filesystem semantics

A cache key is only as correct as the information used to construct it.

A strong hash cannot repair a bad representation.

---

# 82. Edge cases checklist

When designing a real filesystem fingerprint machine, explicitly decide:

### Files
- regular files
- empty files
- large files
- unreadable files
- files changing while being hashed

### Directories
- empty directories
- nested directories
- directory permissions
- whether empty directories matter to the fingerprint

### Symlinks
- symlink itself
- symlink target
- dangling symlink
- symlink cycle
- target outside root
- target changes
- whether symlinks are rejected

### Permissions
- executable bit
- other permission bits
- owner/group metadata
- whether permissions beyond executable state matter

### Paths
- relative versus absolute
- path separators
- `.` and `..`
- case sensitivity differences between filesystems
- Unicode/path normalization policy

### Filesystem behavior
- mount boundaries
- multiple filesystem types
- network filesystems
- concurrent modifications
- special files
- device nodes
- sockets
- FIFOs

### Cache writing
- destination directory creation
- existing object
- temp-file cleanup
- process crash
- concurrent writers
- cross-filesystem rename
- durability requirements
- directory synchronization

---

# 83. One subtle edge case: file changes while hashing

Suppose:

```text
main.go
```

is modified while the fingerprint machine is reading it.

The machine might observe a state that did not correspond to one clean instant in time.

For example:

```text
first half read before edit
second half read after edit
```

Therefore a fingerprint walker is not automatically a snapshot mechanism.

If strict snapshot semantics matter, that is a separate filesystem/concurrency problem.

---

# 84. One subtle edge case: metadata and contents are separate

A file can have:

```text
same contents
different permissions
```

For example:

```text
script.sh
contents = identical
```

but:

```text
version A: executable
version B: not executable
```

If executable status affects program behavior, then the fingerprint must include it.

That is why the `exec` field exists.

---

# 85. One subtle edge case: special files

Not every filesystem object is a regular file.

Examples include:

```text
directory
symlink
FIFO
socket
device node
```

A fingerprint machine must decide what to do with these.

The safest design is not:

```text
whatever happens to work
```

but:

```text
explicit policy for each relevant object type
```

---

# 86. The deepest lesson from the whole exercise

The system has three layers.

## Layer 1 — filesystem semantics

What actually exists?

```text
file
directory
symlink
permissions
paths
metadata
mounts
```

## Layer 2 — canonical serialization

How do we represent that state as bytes?

```text
kind
path
flags
hashes
separators
ordering
```

## Layer 3 — cryptographic hashing

How do we turn those bytes into a compact identity?

```text
SHA-256
```

The important dependency is:

```text
filesystem semantics
        ↓
canonical serialization
        ↓
SHA-256
```

Not:

```text
filesystem
   ↓
SHA-256
```

The middle layer is where many correctness bugs live.

---

# 87. Final mental model

If you remember only one diagram, remember this:

```text
                 FILESYSTEM
                     │
                     │
                     ▼
             classify objects
                     │
          ┌──────────┴──────────┐
          │                     │
      regular file           symlink
          │                     │
   hash file bytes       hash target info
          │                     │
          └──────────┬──────────┘
                     │
                     ▼
                entry records
                     │
                     ▼
                sort by path
                     │
                     ▼
             canonical bytes
                     │
                     ▼
                SHA-256
                     │
                     ▼
              PROJECT KEY
                     │
                     │
                     ▼
              CACHE LOOKUP
                     │
                     ▼
          content-addressed blob
                     │
                     ▼
             temp file write
                     │
                     ▼
              optional Sync
                     │
                     ▼
                  close
                     │
                     ▼
             same-filesystem
                  rename
                     │
                     ▼
             FINAL CACHE OBJECT
```

And the central rule is:

> **Define the filesystem semantics correctly first. Serialize those semantics deterministically. Only then hash the resulting bytes.**

That is the foundation of the entire fingerprint + content-addressed cache design.
