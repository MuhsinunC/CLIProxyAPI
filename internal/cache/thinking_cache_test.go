package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// resetSingleton resets the singleton for testing
func resetSingleton() {
	thinkingCacheInstance = nil
	thinkingCacheOnce = sync.Once{}
	thinkingCacheErr = nil
}

func TestThinkingCacheBasicOperations(t *testing.T) {
	resetSingleton()

	// Create temp directory for test
	tmpDir, err := os.MkdirTemp("", "thinking_cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.ThinkingCacheConfig{
		Enabled:     true,
		MaxMemoryMB: 10, // 10MB for testing
		StoragePath: filepath.Join(tmpDir, "badger"),
	}

	cache, err := GetThinkingCache(cfg)
	if err != nil {
		t.Fatalf("Failed to create thinking cache: %v", err)
	}
	defer cache.Close()

	// Test Set and Get
	testID := "test_conversation_123"
	testData := []byte(`{"type":"thinking","thinking":"","signature":"test_signature"}`)

	cache.Set(testID, testData)

	// Give async writer time to process
	time.Sleep(100 * time.Millisecond)

	retrieved, found := cache.Get(testID)
	if !found {
		t.Fatal("Expected to find cached entry")
	}

	if string(retrieved) != string(testData) {
		t.Errorf("Expected %s, got %s", string(testData), string(retrieved))
	}
}

func TestThinkingCacheLRUEviction(t *testing.T) {
	resetSingleton()

	tmpDir, err := os.MkdirTemp("", "thinking_cache_lru_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Small cache to trigger eviction
	cfg := config.ThinkingCacheConfig{
		Enabled:     true,
		MaxMemoryMB: 1, // 1MB - very small
		StoragePath: filepath.Join(tmpDir, "badger"),
	}

	cache, err := GetThinkingCache(cfg)
	if err != nil {
		t.Fatalf("Failed to create thinking cache: %v", err)
	}
	defer cache.Close()

	// Fill cache with entries
	largeData := make([]byte, 100*1024) // 100KB per entry
	for i := 0; i < 15; i++ { // ~1.5MB total, should trigger eviction
		cache.Set(GenerateConversationID([]byte{byte(i)}), largeData)
	}

	// Give async writer time
	time.Sleep(200 * time.Millisecond)

	// Check stats - RAM should be around maxBytes
	ramEntries, ramBytes, maxBytes := cache.Stats()
	if ramBytes > maxBytes {
		t.Errorf("RAM bytes (%d) exceeds max bytes (%d)", ramBytes, maxBytes)
	}

	t.Logf("LRU eviction test: ramEntries=%d, ramBytes=%d, maxBytes=%d", ramEntries, ramBytes, maxBytes)
}

func TestThinkingCachePersistence(t *testing.T) {
	resetSingleton()

	tmpDir, err := os.MkdirTemp("", "thinking_cache_persist_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dbPath := filepath.Join(tmpDir, "badger")
	testID := "persist_test_conversation"
	testData := []byte(`{"type":"thinking","signature":"persist_test"}`)

	// First cache instance - write data
	{
		cfg := config.ThinkingCacheConfig{
			Enabled:     true,
			MaxMemoryMB: 10,
			StoragePath: dbPath,
		}

		cache, err := GetThinkingCache(cfg)
		if err != nil {
			t.Fatalf("Failed to create first thinking cache: %v", err)
		}

		cache.Set(testID, testData)

		// Give async writer time
		time.Sleep(200 * time.Millisecond)

		cache.Close()
	}

	// Reset singleton
	resetSingleton()

	// Second cache instance - read data
	{
		cfg := config.ThinkingCacheConfig{
			Enabled:     true,
			MaxMemoryMB: 10,
			StoragePath: dbPath,
		}

		cache, err := GetThinkingCache(cfg)
		if err != nil {
			t.Fatalf("Failed to create second thinking cache: %v", err)
		}
		defer cache.Close()

		retrieved, found := cache.Get(testID)
		if !found {
			t.Fatal("Expected to find persisted entry after cache restart")
		}

		if string(retrieved) != string(testData) {
			t.Errorf("Expected %s, got %s", string(testData), string(retrieved))
		}
	}
}

func TestGenerateConversationID(t *testing.T) {
	messages1 := []byte(`[{"role":"user","content":"Hello"}]`)
	messages2 := []byte(`[{"role":"user","content":"Hello"}]`)
	messages3 := []byte(`[{"role":"user","content":"Different"}]`)

	id1 := GenerateConversationID(messages1)
	id2 := GenerateConversationID(messages2)
	id3 := GenerateConversationID(messages3)

	// Same messages should produce same ID
	if id1 != id2 {
		t.Errorf("Same messages should produce same ID: %s != %s", id1, id2)
	}

	// Different messages should produce different ID
	if id1 == id3 {
		t.Errorf("Different messages should produce different ID: %s == %s", id1, id3)
	}

	// ID should be 16 hex characters (64 bits)
	if len(id1) != 16 {
		t.Errorf("Expected ID length 16, got %d", len(id1))
	}
}

func TestGenerateStableConversationID(t *testing.T) {
	// Test that formatting differences don't affect the ID
	messages1 := []byte(`[{"role":"user","content":"Hello World"},{"role":"assistant","content":"Hi!"}]`)
	messages2 := []byte(`[{"role": "user", "content": "Hello World"}, {"role": "assistant", "content": "Hi!"}]`)

	id1 := GenerateStableConversationID(messages1, 1)
	id2 := GenerateStableConversationID(messages2, 1)

	if id1 != id2 {
		t.Errorf("Stable IDs should match despite formatting: %s != %s", id1, id2)
	}

	// Different assistant index should produce different ID
	id3 := GenerateStableConversationID(messages1, 0)
	if id1 == id3 {
		t.Errorf("Different assistant index should produce different ID")
	}
}

func TestThinkingCacheDisabled(t *testing.T) {
	resetSingleton()

	cfg := config.ThinkingCacheConfig{
		Enabled: false,
	}

	cache, err := GetThinkingCache(cfg)
	if err != nil {
		t.Fatalf("Expected no error for disabled cache: %v", err)
	}

	if cache != nil {
		t.Error("Expected nil cache when disabled")
	}
}

func BenchmarkThinkingCacheSet(b *testing.B) {
	resetSingleton()

	tmpDir, err := os.MkdirTemp("", "thinking_cache_bench")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.ThinkingCacheConfig{
		Enabled:     true,
		MaxMemoryMB: 512,
		StoragePath: filepath.Join(tmpDir, "badger"),
	}

	cache, err := GetThinkingCache(cfg)
	if err != nil {
		b.Fatalf("Failed to create thinking cache: %v", err)
	}
	defer cache.Close()

	testData := []byte(`{"type":"thinking","thinking":"","signature":"benchmark_signature_data_here"}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := GenerateConversationID([]byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		cache.Set(id, testData)
	}
}

func BenchmarkThinkingCacheGet(b *testing.B) {
	resetSingleton()

	tmpDir, err := os.MkdirTemp("", "thinking_cache_bench_get")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.ThinkingCacheConfig{
		Enabled:     true,
		MaxMemoryMB: 512,
		StoragePath: filepath.Join(tmpDir, "badger"),
	}

	cache, err := GetThinkingCache(cfg)
	if err != nil {
		b.Fatalf("Failed to create thinking cache: %v", err)
	}
	defer cache.Close()

	// Pre-populate cache
	testData := []byte(`{"type":"thinking","thinking":"","signature":"benchmark_signature_data_here"}`)
	for i := 0; i < 1000; i++ {
		id := GenerateConversationID([]byte{byte(i >> 8), byte(i)})
		cache.Set(id, testData)
	}

	// Wait for async writes
	time.Sleep(500 * time.Millisecond)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := GenerateConversationID([]byte{byte((i % 1000) >> 8), byte(i % 1000)})
		cache.Get(id)
	}
}
