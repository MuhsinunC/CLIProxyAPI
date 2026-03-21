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

	writeEntryToBadger(storeEntry{
		GroupKey:  "claude",
		TextHash:  "abcdef1234567890",
		Signature: "testSig1234567890123456789012345678901234567890123456",
	})

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

	// Verify entries are on disk
	sig1Direct, ok1 := storeGet("claude", "hash1_1234567890")
	if !ok1 || sig1Direct != "sig1_1234567890123456789012345678901234567890123456789" {
		t.Errorf("claude entry not found after restart. sig=%q ok=%v", sig1Direct, ok1)
	}

	sig2Direct, ok2 := storeGet("gemini", "hash2_1234567890")
	if !ok2 || sig2Direct != "sig2_1234567890123456789012345678901234567890123456789" {
		t.Errorf("gemini entry not found after restart. sig=%q ok=%v", sig2Direct, ok2)
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

	storeClear("claude-sonnet-4-5")

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

	CacheSignature("claude-sonnet-4-5", text, sig)

	// Wait for async writer to process
	time.Sleep(200 * time.Millisecond)

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
