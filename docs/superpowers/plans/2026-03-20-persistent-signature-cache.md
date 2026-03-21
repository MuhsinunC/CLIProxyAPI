# Persistent Signature Cache Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add BadgerDB persistence underneath the existing in-memory signature cache so thinking signatures survive server restarts.

**Architecture:** Two-tier cache — existing `sync.Map` as hot layer (3h TTL), BadgerDB as cold/persistent layer (no expiry). Disk fallback on in-memory miss, async write-through on cache set, full load from disk on startup.

**Tech Stack:** Go, BadgerDB v4 (`github.com/dgraph-io/badger/v4`), existing `internal/cache` package

**Spec:** `docs/superpowers/specs/2026-03-20-persistent-signature-cache-design.md`

---

### Task 1: Add BadgerDB dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

- [ ] **Step 1: Add BadgerDB v4 dependency**

```bash
cd /Users/user/Documents/Muhsinun/Projects/GitHub/CLIProxyAPI && go get github.com/dgraph-io/badger/v4
```

- [ ] **Step 2: Tidy modules**

```bash
go mod tidy
```

- [ ] **Step 3: Verify build still works**

```bash
CGO_ENABLED=0 go build ./...
```
Expected: success, no errors

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add BadgerDB v4 for persistent signature cache"
```

---

### Task 2: Create signature_store.go — core BadgerDB persistence layer

**Files:**
- Create: `internal/cache/signature_store.go`

- [ ] **Step 1: Create the file with all BadgerDB persistence code**

Create `internal/cache/signature_store.go` with these components:

```go
package cache

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	log "github.com/sirupsen/logrus"
)

// Package-level singleton state for the persistent signature store.
var (
	signatureStore   *badger.DB
	storeWriteChan   chan storeEntry
	storeDoneChan    chan struct{}
	storeInitOnce    sync.Once
	storeInitialized bool
)

// storeEntry represents a signature entry to be written to BadgerDB.
type storeEntry struct {
	GroupKey  string
	TextHash string
	Signature string
}

// InitSignatureStore opens a BadgerDB at the given path and loads all
// persisted signatures into the in-memory sync.Map cache.
// Safe to call multiple times — only the first call takes effect.
// If path is empty, persistence is disabled (no-op).
func InitSignatureStore(path string) error {
	if path == "" {
		return nil
	}

	var initErr error
	storeInitOnce.Do(func() {
		opts := badger.DefaultOptions(path).
			WithValueLogFileSize(64 << 20).       // 64MB value log files
			WithNumMemtables(2).
			WithNumLevelZeroTables(2).
			WithNumLevelZeroTablesStall(4).
			WithValueThreshold(1024).              // values >1KB go to value log
			WithSyncWrites(false).                 // async — sync.Map provides immediate durability
			WithDetectConflicts(false).
			WithLogger(nil)                        // suppress BadgerDB's verbose logging

		db, err := badger.Open(opts)
		if err != nil {
			initErr = fmt.Errorf("failed to open signature store at %s: %w", path, err)
			return
		}

		signatureStore = db
		storeWriteChan = make(chan storeEntry, 1000)
		storeDoneChan = make(chan struct{})

		// Load persisted entries into in-memory cache
		loaded := storeLoadAll()
		if loaded > 0 {
			log.Infof("[SIGNATURE-STORE] Loaded %d signatures from disk", loaded)
		}

		// Start background goroutines
		go asyncStoreWriter()
		go runStoreGC()

		storeInitialized = true
		log.Infof("[SIGNATURE-STORE] Initialized at %s", path)
	})
	return initErr
}

// CloseSignatureStore gracefully shuts down the persistent store.
// Drains pending writes, stops background goroutines, and closes BadgerDB.
func CloseSignatureStore() {
	if !storeInitialized || signatureStore == nil {
		return
	}
	storeInitialized = false

	// Signal background goroutines to stop
	close(storeDoneChan)

	// Close BadgerDB (asyncStoreWriter drains before this completes)
	// Give the writer a moment to drain
	time.Sleep(100 * time.Millisecond)

	if err := signatureStore.Close(); err != nil {
		log.Warnf("[SIGNATURE-STORE] Error closing: %v", err)
	}
	log.Info("[SIGNATURE-STORE] Closed")
}

// storeGet retrieves a signature from BadgerDB by groupKey and textHash.
// Returns the signature and true if found, empty string and false otherwise.
func storeGet(groupKey, textHash string) (string, bool) {
	if signatureStore == nil {
		return "", false
	}
	key := groupKey + "/" + textHash
	var sig string
	err := signatureStore.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			// Value format: "{signature}\x00{unix_timestamp}"
			parts := strings.SplitN(string(val), "\x00", 2)
			sig = parts[0]
			return nil
		})
	})
	if err != nil {
		return "", false
	}
	return sig, sig != ""
}

// storePut enqueues a signature for async write to BadgerDB.
// Non-blocking: silently drops if the write channel is full.
func storePut(groupKey, textHash, signature string) {
	select {
	case storeWriteChan <- storeEntry{GroupKey: groupKey, TextHash: textHash, Signature: signature}:
	default:
		// Channel full — drop this write. The entry is still in sync.Map.
	}
}

// storeClear removes entries from BadgerDB.
// If modelName is empty, drops all data. Otherwise, deletes entries
// matching the model's group key prefix.
func storeClear(modelName string) {
	if signatureStore == nil {
		return
	}
	if modelName == "" {
		if err := signatureStore.DropAll(); err != nil {
			log.Warnf("[SIGNATURE-STORE] Error clearing all entries: %v", err)
		}
		return
	}
	groupKey := GetModelGroup(modelName)
	prefix := []byte(groupKey + "/")
	err := signatureStore.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // keys only
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			if err := txn.Delete(it.Item().KeyCopy(nil)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.Warnf("[SIGNATURE-STORE] Error clearing entries for %s: %v", groupKey, err)
	}
}

// storeLoadAll iterates all BadgerDB entries and loads them into the
// in-memory sync.Map cache. Returns the number of entries loaded.
func storeLoadAll() int {
	if signatureStore == nil {
		return 0
	}
	count := 0
	err := signatureStore.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			// Parse key: "{groupKey}/{textHash}"
			slashIdx := strings.IndexByte(key, '/')
			if slashIdx < 0 {
				continue // malformed key
			}
			groupKey := key[:slashIdx]
			textHash := key[slashIdx+1:]

			// Parse value: "{signature}\x00{unix_timestamp}"
			err := item.Value(func(val []byte) error {
				parts := strings.SplitN(string(val), "\x00", 2)
				if len(parts) == 0 || parts[0] == "" {
					return nil
				}
				signature := parts[0]

				// Insert into in-memory cache
				sc := getOrCreateGroupCache(groupKey)
				sc.mu.Lock()
				sc.entries[textHash] = SignatureEntry{
					Signature: signature,
					Timestamp: time.Now(),
				}
				sc.mu.Unlock()
				count++
				return nil
			})
			if err != nil {
				log.Debugf("[SIGNATURE-STORE] Error reading entry %s: %v", key, err)
			}
		}
		return nil
	})
	if err != nil {
		log.Warnf("[SIGNATURE-STORE] Error loading entries: %v", err)
	}
	return count
}

// storeGetAndPromote checks BadgerDB for a signature and promotes it
// to the in-memory sync.Map on hit. Returns the signature if found.
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

// asyncStoreWriter is a background goroutine that drains the write channel
// and persists entries to BadgerDB.
func asyncStoreWriter() {
	for {
		select {
		case entry := <-storeWriteChan:
			writeEntryToBadger(entry)
		case <-storeDoneChan:
			// Drain remaining entries before exit
			for {
				select {
				case entry := <-storeWriteChan:
					writeEntryToBadger(entry)
				default:
					return
				}
			}
		}
	}
}

// writeEntryToBadger writes a single entry to BadgerDB.
func writeEntryToBadger(entry storeEntry) {
	if signatureStore == nil {
		return
	}
	key := entry.GroupKey + "/" + entry.TextHash
	value := entry.Signature + "\x00" + strconv.FormatInt(time.Now().Unix(), 10)
	err := signatureStore.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), []byte(value))
	})
	if err != nil {
		log.Debugf("[SIGNATURE-STORE] Error writing entry %s: %v", key, err)
	}
}

// runStoreGC periodically triggers BadgerDB's value log garbage collection.
func runStoreGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			for {
				if signatureStore == nil {
					return
				}
				err := signatureStore.RunValueLogGC(0.5)
				if err != nil {
					break // no more GC needed
				}
			}
		case <-storeDoneChan:
			return
		}
	}
}
```

- [ ] **Step 2: Verify it compiles**

```bash
CGO_ENABLED=0 go build ./internal/cache/...
```
Expected: success

- [ ] **Step 3: Commit**

```bash
git add internal/cache/signature_store.go
git commit -m "feat: add BadgerDB persistence layer for signature cache

Implements InitSignatureStore, CloseSignatureStore, and internal helpers
for disk-based signature caching with async writes and startup loading."
```

---

### Task 3: Modify signature_cache.go — hook disk fallback and async write

**Files:**
- Modify: `internal/cache/signature_cache.go`

- [ ] **Step 1: Add disk fallback to GetCachedSignature**

In `GetCachedSignature`, add `storeGetAndPromote` calls at the three miss/expiry points. The modified function should be:

```go
func GetCachedSignature(modelName, text string) string {
	groupKey := GetModelGroup(modelName)

	if text == "" {
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}

	textHash := hashText(text)

	val, ok := signatureCache.Load(groupKey)
	if !ok {
		// Group-level miss — try disk
		if sig := storeGetAndPromote(groupKey, textHash); sig != "" {
			return sig
		}
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}
	sc := val.(*groupCache)

	now := time.Now()

	sc.mu.Lock()
	entry, exists := sc.entries[textHash]
	if !exists {
		sc.mu.Unlock()
		// Entry-level miss — try disk
		if sig := storeGetAndPromote(groupKey, textHash); sig != "" {
			return sig
		}
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}
	if now.Sub(entry.Timestamp) > SignatureCacheTTL {
		delete(sc.entries, textHash)
		sc.mu.Unlock()
		// TTL expiry — try disk (disk has no TTL)
		if sig := storeGetAndPromote(groupKey, textHash); sig != "" {
			return sig
		}
		if groupKey == "gemini" {
			return "skip_thought_signature_validator"
		}
		return ""
	}

	// Refresh TTL on access (sliding expiration).
	entry.Timestamp = now
	sc.entries[textHash] = entry
	sc.mu.Unlock()

	return entry.Signature
}
```

Key change: `textHash` is now computed once at the top (moved from after the `signatureCache.Load` call) so it's available for the group-level miss disk fallback.

- [ ] **Step 2: Add async disk write to CacheSignature**

Add the `storePut` call after the existing sync.Map write:

```go
func CacheSignature(modelName, text, signature string) {
	if text == "" || signature == "" {
		return
	}
	if len(signature) < MinValidSignatureLen {
		return
	}

	groupKey := GetModelGroup(modelName)
	textHash := hashText(text)
	sc := getOrCreateGroupCache(groupKey)
	sc.mu.Lock()
	sc.entries[textHash] = SignatureEntry{
		Signature: signature,
		Timestamp: time.Now(),
	}
	sc.mu.Unlock()

	// Persist to disk asynchronously
	if storeInitialized {
		storePut(groupKey, textHash, signature)
	}
}
```

- [ ] **Step 3: Add disk clear to ClearSignatureCache**

Add the `storeClear` call after the existing sync.Map clear:

```go
func ClearSignatureCache(modelName string) {
	if modelName == "" {
		signatureCache.Range(func(key, _ any) bool {
			signatureCache.Delete(key)
			return true
		})
	} else {
		groupKey := GetModelGroup(modelName)
		signatureCache.Delete(groupKey)
	}

	// Clear disk entries
	if storeInitialized {
		storeClear(modelName)
	}
}
```

- [ ] **Step 4: Verify existing tests still pass**

```bash
cd /Users/user/Documents/Muhsinun/Projects/GitHub/CLIProxyAPI && go test ./internal/cache/... -v
```
Expected: all existing tests pass (store is not initialized, so disk paths are no-ops)

- [ ] **Step 5: Commit**

```bash
git add internal/cache/signature_cache.go
git commit -m "feat: hook disk fallback and async write into signature cache

GetCachedSignature now falls back to BadgerDB on sync.Map miss.
CacheSignature writes through to disk asynchronously.
ClearSignatureCache clears both in-memory and disk entries.
All changes are guarded by storeInitialized flag — zero impact
when persistence is not configured."
```

---

### Task 4: Add config field and wire up init/shutdown

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/server/main.go`
- Modify: `.gitignore`

- [ ] **Step 1: Add SignatureCachePath to Config struct**

In `internal/config/config.go`, add the field after `Payload`:

```go
	// SignatureCachePath is the directory path for persistent signature cache storage (BadgerDB).
	// When set, thinking signatures survive server restarts. Leave empty to disable.
	SignatureCachePath string `yaml:"signature-cache-path" json:"signature-cache-path"`
```

- [ ] **Step 2: Wire up init in cmd/server/main.go**

Add the import for the cache package at the top of `cmd/server/main.go`:

```go
"github.com/router-for-me/CLIProxyAPI/v6/internal/cache"
```

After the auth dir resolution block (after line 435 `cfg.AuthDir = resolvedAuthDir`), add:

```go
	// Initialize persistent signature cache if configured
	if cfg.SignatureCachePath != "" {
		if err := cache.InitSignatureStore(cfg.SignatureCachePath); err != nil {
			log.Warnf("Failed to initialize signature cache store: %v", err)
		}
		defer cache.CloseSignatureStore()
	}
```

- [ ] **Step 3: Add gitignore entry**

Add `signature_cache_badger/` to `.gitignore` in the "Storage backends" section:

```
# Storage backends
pgstore/*
gitstore/*
objectstore/*
signature_cache_badger/
```

- [ ] **Step 4: Verify build**

```bash
CGO_ENABLED=0 go build ./...
```
Expected: success

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go cmd/server/main.go .gitignore
git commit -m "feat: wire up persistent signature cache config and init

Add signature-cache-path YAML config field.
Initialize BadgerDB store on startup, close on shutdown.
Add signature_cache_badger/ to gitignore."
```

---

### Task 5: Write tests for persistent signature cache

**Files:**
- Create: `internal/cache/signature_store_test.go`

- [ ] **Step 1: Create test file**

```go
package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// resetStore clears the store singleton state for test isolation.
func resetStore() {
	if storeInitialized {
		CloseSignatureStore()
	}
	signatureStore = nil
	storeWriteChan = nil
	storeDoneChan = nil
	storeInitOnce = sync.Once{}
	storeInitialized = false
	ClearSignatureCache("")
}

func TestInitSignatureStore_CreatesAndOpens(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	err := InitSignatureStore(dir)
	if err != nil {
		t.Fatalf("InitSignatureStore failed: %v", err)
	}
	if !storeInitialized {
		t.Fatal("storeInitialized should be true")
	}
	if signatureStore == nil {
		t.Fatal("signatureStore should not be nil")
	}
}

func TestInitSignatureStore_EmptyPath_Disabled(t *testing.T) {
	resetStore()
	defer resetStore()

	err := InitSignatureStore("")
	if err != nil {
		t.Fatalf("InitSignatureStore with empty path should succeed: %v", err)
	}
	if storeInitialized {
		t.Fatal("storeInitialized should be false for empty path")
	}
}

func TestInitSignatureStore_DoubleInit_SafeNoop(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	err := InitSignatureStore(dir)
	if err != nil {
		t.Fatalf("First init failed: %v", err)
	}

	// Second init should be a no-op (sync.Once)
	err = InitSignatureStore(dir)
	if err != nil {
		t.Fatalf("Second init should succeed as no-op: %v", err)
	}
}

func TestStoreWriteAndRead(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Write directly to BadgerDB
	writeEntryToBadger(storeEntry{
		GroupKey:  "claude",
		TextHash:  "abcdef1234567890",
		Signature: "testSig1234567890123456789012345678901234567890123456",
	})

	// Read back
	sig, ok := storeGet("claude", "abcdef1234567890")
	if !ok {
		t.Fatal("storeGet should find the entry")
	}
	if sig != "testSig1234567890123456789012345678901234567890123456" {
		t.Errorf("Unexpected signature: %s", sig)
	}
}

func TestStoreWriteAndRead_NotFound(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	sig, ok := storeGet("claude", "nonexistent1234567")
	if ok || sig != "" {
		t.Errorf("Expected not found, got sig=%q ok=%v", sig, ok)
	}
}

func TestStoreRestartSurvival(t *testing.T) {
	resetStore()
	dir := filepath.Join(t.TempDir(), "test_store")

	// Phase 1: Write entries
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	writeEntryToBadger(storeEntry{
		GroupKey:  "claude",
		TextHash:  "hash1_1234567890",
		Signature: "sig1_1234567890123456789012345678901234567890123456789",
	})
	writeEntryToBadger(storeEntry{
		GroupKey:  "gemini",
		TextHash:  "hash2_1234567890",
		Signature: "sig2_1234567890123456789012345678901234567890123456789",
	})

	// Simulate restart
	resetStore()

	// Phase 2: Reopen and verify entries loaded into sync.Map
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Re-init failed: %v", err)
	}

	// Entries should be in sync.Map now (loaded on startup)
	sig1 := GetCachedSignature("claude-sonnet-4-5", "")
	// Can't use GetCachedSignature with empty text for claude (returns ""),
	// so verify via direct storeGet
	sig1Direct, ok1 := storeGet("claude", "hash1_1234567890")
	if !ok1 || sig1Direct != "sig1_1234567890123456789012345678901234567890123456789" {
		t.Errorf("Phase 2: claude entry not found after restart. sig=%q ok=%v", sig1Direct, ok1)
	}

	sig2Direct, ok2 := storeGet("gemini", "hash2_1234567890")
	if !ok2 || sig2Direct != "sig2_1234567890123456789012345678901234567890123456789" {
		t.Errorf("Phase 2: gemini entry not found after restart. sig=%q ok=%v", sig2Direct, ok2)
	}

	// Verify entries were loaded into sync.Map
	val, loaded := signatureCache.Load("claude")
	if !loaded {
		t.Fatal("claude group should be loaded into sync.Map")
	}
	sc := val.(*groupCache)
	sc.mu.RLock()
	entry, exists := sc.entries["hash1_1234567890"]
	sc.mu.RUnlock()
	if !exists {
		t.Fatal("hash1 should exist in sync.Map claude group")
	}
	if entry.Signature != "sig1_1234567890123456789012345678901234567890123456789" {
		t.Errorf("Unexpected signature in sync.Map: %s", entry.Signature)
	}

	_ = sig1 // used above for coverage
	resetStore()
}

func TestStoreClear_All(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	writeEntryToBadger(storeEntry{GroupKey: "claude", TextHash: "h1", Signature: "sig1234567890123456789012345678901234567890123456789012"})
	writeEntryToBadger(storeEntry{GroupKey: "gemini", TextHash: "h2", Signature: "sig1234567890123456789012345678901234567890123456789012"})

	storeClear("")

	if _, ok := storeGet("claude", "h1"); ok {
		t.Error("claude entry should be cleared")
	}
	if _, ok := storeGet("gemini", "h2"); ok {
		t.Error("gemini entry should be cleared")
	}
}

func TestStoreClear_ByModelGroup(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	writeEntryToBadger(storeEntry{GroupKey: "claude", TextHash: "h1", Signature: "sig1234567890123456789012345678901234567890123456789012"})
	writeEntryToBadger(storeEntry{GroupKey: "gemini", TextHash: "h2", Signature: "sig1234567890123456789012345678901234567890123456789012"})

	storeClear("claude-sonnet-4-5") // should clear "claude" group

	if _, ok := storeGet("claude", "h1"); ok {
		t.Error("claude entry should be cleared")
	}
	if _, ok := storeGet("gemini", "h2"); !ok {
		t.Error("gemini entry should NOT be cleared")
	}
}

func TestAsyncWriteAndRead_Integration(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	text := "This is thinking text for async test"
	sig := "asyncSig12345678901234567890123456789012345678901234567"

	// Use the public API which triggers async write
	CacheSignature("claude-sonnet-4-5", text, sig)

	// Wait for async writer to process
	time.Sleep(200 * time.Millisecond)

	// Verify it's on disk
	textHash := hashText(text)
	diskSig, ok := storeGet("claude", textHash)
	if !ok {
		t.Fatal("Entry should be on disk after async write")
	}
	if diskSig != sig {
		t.Errorf("Disk signature mismatch: got %q, want %q", diskSig, sig)
	}
}

func TestDiskFallback_OnMemoryMiss(t *testing.T) {
	resetStore()
	defer resetStore()

	dir := filepath.Join(t.TempDir(), "test_store")
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	text := "Thinking text for fallback test"
	sig := "fallbackSig1234567890123456789012345678901234567890123"
	textHash := hashText(text)

	// Write directly to disk (bypassing sync.Map)
	writeEntryToBadger(storeEntry{GroupKey: "claude", TextHash: textHash, Signature: sig})

	// sync.Map doesn't have it — GetCachedSignature should fall back to disk
	result := GetCachedSignature("claude-sonnet-4-5", text)
	if result != sig {
		t.Errorf("Expected disk fallback to return %q, got %q", sig, result)
	}

	// After fallback, it should be promoted to sync.Map
	val, loaded := signatureCache.Load("claude")
	if !loaded {
		t.Fatal("claude group should exist in sync.Map after promotion")
	}
	sc := val.(*groupCache)
	sc.mu.RLock()
	entry, exists := sc.entries[textHash]
	sc.mu.RUnlock()
	if !exists || entry.Signature != sig {
		t.Error("Entry should be promoted to sync.Map after disk fallback")
	}
}

func TestGracefulDegradation_StoreNotInitialized(t *testing.T) {
	resetStore()
	defer resetStore()

	// Don't init store — all disk operations should be no-ops
	text := "Some thinking text"
	sig := "validSig1234567890123456789012345678901234567890123456"

	CacheSignature("claude-sonnet-4-5", text, sig)
	result := GetCachedSignature("claude-sonnet-4-5", text)
	if result != sig {
		t.Errorf("In-memory cache should still work without store: got %q", result)
	}

	ClearSignatureCache("")
	result = GetCachedSignature("claude-sonnet-4-5", text)
	if result != "" {
		t.Error("Clear should still work without store")
	}
}

func TestStoreLoadAll_PopulatesSyncMap(t *testing.T) {
	resetStore()
	dir := filepath.Join(t.TempDir(), "test_store")

	// Phase 1: Create entries on disk
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	for i := 0; i < 10; i++ {
		writeEntryToBadger(storeEntry{
			GroupKey:  "claude",
			TextHash:  fmt.Sprintf("hash%02d_123456789", i),
			Signature: fmt.Sprintf("sig%02d_12345678901234567890123456789012345678901234567", i),
		})
	}
	resetStore()

	// Phase 2: Reopen — all 10 should be in sync.Map
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Re-init failed: %v", err)
	}

	val, loaded := signatureCache.Load("claude")
	if !loaded {
		t.Fatal("claude group should be loaded")
	}
	sc := val.(*groupCache)
	sc.mu.RLock()
	count := len(sc.entries)
	sc.mu.RUnlock()
	if count != 10 {
		t.Errorf("Expected 10 entries in sync.Map, got %d", count)
	}

	resetStore()
}

func TestStorePersistence_AcrossDirectoryReuse(t *testing.T) {
	resetStore()

	// Use a fixed temp dir that persists across phases
	dir := filepath.Join(os.TempDir(), "sig_cache_test_persist")
	os.RemoveAll(dir)
	defer os.RemoveAll(dir)

	// Phase 1: Write
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	text := "Persistent thinking text"
	sig := "persistSig123456789012345678901234567890123456789012345"
	CacheSignature("claude-opus-4-6", text, sig)
	time.Sleep(200 * time.Millisecond) // wait for async write
	resetStore()

	// Phase 2: Read
	if err := InitSignatureStore(dir); err != nil {
		t.Fatalf("Re-init failed: %v", err)
	}
	result := GetCachedSignature("claude-opus-4-6", text)
	if result != sig {
		t.Errorf("Expected %q after restart, got %q", sig, result)
	}
	resetStore()
}
```

- [ ] **Step 2: Run tests**

```bash
cd /Users/user/Documents/Muhsinun/Projects/GitHub/CLIProxyAPI && go test ./internal/cache/... -v -count=1
```
Expected: all tests pass

- [ ] **Step 3: Commit**

```bash
git add internal/cache/signature_store_test.go
git commit -m "test: add comprehensive tests for persistent signature cache

Tests cover: init/close lifecycle, write/read round-trip, restart
survival, startup loading, async write integration, disk fallback,
graceful degradation, and clear operations."
```

---

### Task 6: Run full test suite and verify CGO_ENABLED=0 build

**Files:** (none — verification only)

- [ ] **Step 1: Build with CGO disabled**

```bash
cd /Users/user/Documents/Muhsinun/Projects/GitHub/CLIProxyAPI && CGO_ENABLED=0 go build ./...
```
Expected: success

- [ ] **Step 2: Run cache package tests**

```bash
go test ./internal/cache/... -v -count=1
```
Expected: all pass

- [ ] **Step 3: Run full test suite**

```bash
go test ./... -count=1 -timeout 5m 2>&1 | tail -50
```
Expected: no new failures (pre-existing failures in sdk/cliproxy/auth are known)

- [ ] **Step 4: Verify config.example.yaml doesn't need update**

Check if `config.example.yaml` exists and whether it should document the new field. If it exists, add a commented example:

```yaml
# signature-cache-path: "signature_cache_badger"
```

---

### Task 7: Final commit and push

- [ ] **Step 1: Check status**

```bash
git status && git log --oneline -5
```

- [ ] **Step 2: Push to remote**

```bash
git push origin MuhsinunC/dev
```
