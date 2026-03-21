# Persistent Signature Cache Design

**Date**: 2026-03-20
**Status**: Draft
**Branch**: MuhsinunC/dev

## Problem

The upstream in-memory signature cache (`sync.Map` in `internal/cache/signature_cache.go`) loses all cached thinking signatures when the proxy server restarts. This causes thinking block validation failures when continuing a conversation after a server restart, because the cryptographic signatures needed to verify thinking blocks are gone.

The previous fork implementation solved this with a persistent cache (SQLite, then BadgerDB), but those changes were dropped when rebasing onto the latest upstream.

## Goal

Add a BadgerDB persistence layer underneath the existing in-memory `sync.Map` signature cache. The in-memory cache remains the fast path; BadgerDB acts as a durable fallback that survives server restarts.

## Non-Goals

- Replacing the upstream's in-memory cache logic (TTL, cleanup, model grouping)
- Adding LRU eviction or memory limits (the data model is small enough — ~500 bytes per entry — that thousands of entries fit in a few MB)
- Conversation-scoped keys (signatures are content-scoped, not conversation-scoped — confirmed by upstream's production implementation and Anthropic's docs)
- Migration from old ThinkingCache databases (the old cache stored different data — full thinking block JSON blobs keyed by conversation hash)

## Architecture

```
                    ┌─────────────────────────────┐
                    │     Existing Callers         │
                    │  (antigravity translators)   │
                    └──────────┬──────────────────┘
                               │ unchanged API
                    ┌──────────▼──────────────────┐
                    │  Package-level API           │
                    │  CacheSignature()            │
                    │  GetCachedSignature()        │
                    │  ClearSignatureCache()       │
                    │  HasValidSignature()         │
                    └──────────┬──────────────────┘
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                     │
   ┌──────▼──────┐    ┌───────▼───────┐    ┌───────▼────────┐
   │  sync.Map   │    │  Async Write  │    │  BadgerDB      │
   │  (hot cache)│    │  Channel      │    │  (cold/disk)   │
   │  3h TTL     │    │  buffered     │    │  no expiry     │
   │  in-memory  │    │  1000 cap     │    │  persistent    │
   └─────────────┘    └───────────────┘    └────────────────┘
```

### Data Model

Each cache entry:
- **Key**: `{modelGroup}/{textHash}` — where modelGroup is "claude", "gemini", or "gpt", and textHash is SHA256 of the thinking text truncated to 16 hex chars
- **Value**: `{signature}\x00{unix_timestamp}` — the raw signature string concatenated with a null byte separator and the Unix timestamp
- **Typical size**: ~250-1050 bytes per entry

### Read Path (`GetCachedSignature`)

1. Check `sync.Map` (existing behavior) → hit? Return immediately
2. Miss? If BadgerDB store is initialized, query BadgerDB with key `{groupKey}/{textHash}`
3. Found? Promote entry to `sync.Map` (with current timestamp for TTL), return signature
4. Not found? Return empty string (falls back to client signature as today)

### Write Path (`CacheSignature`)

1. Write to `sync.Map` immediately (existing behavior, unchanged)
2. If BadgerDB store is initialized, send `{groupKey, textHash, signature}` to buffered async write channel
3. Background goroutine drains channel and writes to BadgerDB

### Startup (`InitSignatureStore`)

1. Open BadgerDB at configured path (create directory if needed)
2. Iterate all entries in BadgerDB
3. Load each entry into `sync.Map` (warming the in-memory cache)
4. Start background goroutines: async writer + BadgerDB GC

### Shutdown (`CloseSignatureStore`)

1. Signal background goroutines to stop
2. Drain remaining items from async write channel to BadgerDB
3. Close BadgerDB handle

## Detailed Design

### New File: `internal/cache/signature_store.go`

Contains all BadgerDB-related code, cleanly separated from the upstream's `signature_cache.go`.

**Package-level state** (singleton pattern):
```go
var (
    signatureStore     *badger.DB
    storeWriteChan     chan storeEntry
    storeDoneChan      chan struct{}
    storeInitOnce      sync.Once
    storeInitialized   bool
)

type storeEntry struct {
    GroupKey  string
    TextHash string
    Signature string
}
```

**`InitSignatureStore(path string) error`**:
- Uses `sync.Once` to prevent double initialization
- Opens BadgerDB with tuned options (from old implementation):
  - `ValueLogFileSize = 64 << 20` (64MB)
  - `NumMemtables = 2`
  - `NumLevelZeroTables = 2`
  - `NumLevelZeroTablesStall = 4`
  - `ValueThreshold = 1024`
  - `SyncWrites = false` (in-memory cache provides immediate durability)
  - `DetectConflicts = false`
  - `Logger = nil` (suppress BadgerDB's verbose logging)
- Loads all entries from BadgerDB into `sync.Map`
- Creates buffered write channel (capacity 1000)
- Starts async writer goroutine
- Starts GC goroutine (every 5 minutes, `RunValueLogGC(0.5)`)

**`CloseSignatureStore()`**:
- Signals done channel
- Waits for writer goroutine to drain
- Closes BadgerDB

**`storeGet(groupKey, textHash string) (string, bool)`**:
- Reads from BadgerDB: key = `{groupKey}/{textHash}`
- Parses value: splits on `\x00` to extract signature and timestamp
- Returns signature and true if found, empty and false if not

**`storePut(groupKey, textHash, signature string)`**:
- Sends entry to write channel (non-blocking; drops if channel full to avoid backpressure)

**`storeClear(modelName string)`**:
- If `modelName` is empty: drop and recreate all BadgerDB data (call `db.DropAll()`)
- If `modelName` is non-empty: compute `groupKey = GetModelGroup(modelName)`, then iterate and delete all BadgerDB keys with prefix `{groupKey}/`

**`storeLoadAll()`**:
- Note: calls `getOrCreateGroupCache(groupKey)` which triggers `cacheCleanupOnce.Do(startCacheCleanup)` on first call — the cleanup goroutine starts during `InitSignatureStore`. This is the desired behavior: cleanup should be active as soon as entries are loaded.
- Iterates all BadgerDB entries
- For each: parse key into groupKey and textHash, parse value into signature
- Call `getOrCreateGroupCache(groupKey)` and insert into its entries map
- Logs count of loaded entries

**`asyncStoreWriter()`**:
- Reads from write channel
- Writes to BadgerDB: key = `{groupKey}/{textHash}`, value = `{signature}\x00{timestamp}`
- On done signal: drains remaining channel entries, then returns

**`runStoreGC()`**:
- Every 5 minutes, calls `signatureStore.RunValueLogGC(0.5)` in a loop until it returns an error (no more GC needed)
- Stops on done signal

### Modifications to `internal/cache/signature_cache.go`

Minimal changes — two hooks added to existing functions:

**`GetCachedSignature`** — add disk fallback via a helper function.

The existing function has multiple early-return branches for Gemini (`"skip_thought_signature_validator"`) at both the group-level miss (sync.Map Load fails) and the entry-level miss (textHash not in groupCache). Rather than adding disk fallback code to each branch, extract a helper:

```go
// storeGetAndPromote checks BadgerDB and promotes to sync.Map on hit.
// Returns the signature if found, empty string if not.
func storeGetAndPromote(groupKey, textHash string) string {
    if !storeInitialized {
        return ""
    }
    sig, ok := storeGet(groupKey, textHash)
    if !ok {
        return ""
    }
    // Promote to sync.Map
    sc := getOrCreateGroupCache(groupKey)
    sc.mu.Lock()
    sc.entries[textHash] = SignatureEntry{Signature: sig, Timestamp: time.Now()}
    sc.mu.Unlock()
    return sig
}
```

Insert disk fallback calls at both miss points in `GetCachedSignature`:
1. **Group-level miss** (line ~129-135, sync.Map Load returns `!ok`): Before the Gemini sentinel return, try `storeGetAndPromote(groupKey, textHash)`. If non-empty, return it.
2. **Entry-level miss** (line ~143-150, textHash not in groupCache entries): Before the Gemini sentinel return, try `storeGetAndPromote(groupKey, textHash)`. If non-empty, return it.
3. **TTL expiry** (line ~151-158, entry exists but expired): Before the Gemini sentinel return, try disk (the disk entry has no TTL). If non-empty, promote and return it.

**`CacheSignature`** — add async disk write after sync.Map write:
```go
// After existing sync.Map write...
if storeInitialized {
    storePut(groupKey, textHash, signature)
}
```

**`ClearSignatureCache`** — optionally clear disk entries:
```go
// After existing sync.Map clear...
if storeInitialized {
    storeClear(modelName)
}
```

### Configuration

Add to `internal/config/config.go` in the `Config` struct:
```go
SignatureCachePath string `yaml:"signature-cache-path" json:"signature-cache-path"`
```

Default: empty string (disabled). When set, enables the persistent cache at the specified directory path.

### Wiring in `cmd/server/main.go`

```go
// After config is loaded, before server starts:
if cfg.SignatureCachePath != "" {
    if err := cache.InitSignatureStore(cfg.SignatureCachePath); err != nil {
        log.Warnf("Failed to initialize signature cache store: %v", err)
    }
}

// On shutdown:
cache.CloseSignatureStore()
```

### Dependency

Add `github.com/dgraph-io/badger/v4` to `go.mod`. This is a pure Go dependency — compatible with `CGO_ENABLED=0`.

## Key Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Storage backend | BadgerDB v4 | Pure Go (CGO_ENABLED=0), LSM tree handles unbounded growth, proven in this codebase before |
| Disk TTL | None (entries persist forever) | Whole point is surviving restarts; pruning by age can be added later if needed |
| Key format on disk | `{groupKey}/{textHash}` | Mirrors sync.Map structure, allows group-scoped operations |
| Value format on disk | `{signature}\x00{timestamp}` | Simple, minimal overhead, no serialization dependency |
| Write strategy | Async channel (cap 1000), non-blocking put | Never block the response path; drop writes if channel full rather than apply backpressure |
| Startup loading | Load all entries into sync.Map | Dataset is small enough; ensures in-memory cache is warm immediately |
| Graceful shutdown | Drain write channel before closing BadgerDB | No writes are lost |
| Integration approach | Modify existing functions with `if storeInitialized` guards | Minimal diff, zero impact when store is not configured |
| GC goroutine | Every 5 minutes, RunValueLogGC(0.5) | Required for BadgerDB to reclaim value log space |
| Config | Optional `signature-cache-path` in YAML | Disabled by default; users opt in by setting a path |

## Error Handling

- **BadgerDB init failure**: Log warning, continue without persistence (graceful degradation)
- **BadgerDB read failure**: Log warning, treat as cache miss (fall through to client signature)
- **BadgerDB write failure**: Log warning, entry still exists in sync.Map (no data loss for current session)
- **Write channel full**: Drop the write silently (entry is in sync.Map; will be written on next access)
- **BadgerDB GC failure**: Logged at debug level, retried on next tick

## Testing Strategy

1. **Unit tests** (`internal/cache/signature_store_test.go`):
   - Init/close lifecycle
   - Write + read round-trip
   - Restart survival (close store, reopen, verify entries persist)
   - Startup loading into sync.Map
   - Async write drain on shutdown
   - Graceful degradation when store not initialized

2. **Integration with existing tests**: Run existing `signature_cache_test.go` — should pass unchanged since store is not initialized in those tests

3. **Build verification**: `go build ./...` with CGO_ENABLED=0

## Files Changed

| File | Change |
|---|---|
| `internal/cache/signature_store.go` | **New** — BadgerDB persistence layer |
| `internal/cache/signature_store_test.go` | **New** — Tests for persistence layer |
| `internal/cache/signature_cache.go` | **Modify** — Add disk fallback + async write hooks |
| `internal/config/config.go` | **Modify** — Add `SignatureCachePath` field |
| `cmd/server/main.go` | **Modify** — Wire up init/shutdown |
| `go.mod` / `go.sum` | **Modify** — Add BadgerDB dependency |
| `.gitignore` | **Modify** — Add `signature_cache_badger/` pattern |

## Future Considerations

- **Age-based pruning**: Add a CLI flag or config option to prune entries older than N days
- **Cache statistics**: Log hit/miss rates for monitoring
- **Export/import**: Allow backing up and restoring the cache file
