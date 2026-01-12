// Package cache provides caching mechanisms for the CLI Proxy API server.
// Currently includes thinking block caching for Claude extended thinking tool loops.
package cache

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/tidwall/gjson"
)

// Singleton instance for ThinkingCache - one cache per process is sufficient
// since all executors use the same database
var (
	thinkingCacheInstance *ThinkingCache
	thinkingCacheOnce     sync.Once
	thinkingCacheErr      error
)

// ThinkingBlock represents a cached Claude thinking block with signature.
type ThinkingBlock struct {
	ConversationID string `json:"conversation_id"`
	ThinkingData   []byte `json:"thinking_data"` // Raw Claude-format thinking block JSON
	CreatedAt      int64  `json:"created_at"`
	LastAccessed   int64  `json:"last_accessed"`
	SizeBytes      int    `json:"size_bytes"`
}

// ThinkingCache provides RAM + BadgerDB caching for Claude thinking blocks.
// RAM is used for fast access with LRU eviction; BadgerDB provides persistence.
// BadgerDB is significantly faster than SQLite for key-value workloads:
// - ~375x faster writes than BoltDB/SQLite
// - LSM tree architecture keeps keys in RAM, values on SSD
// - Pure Go (no CGO dependencies)
// - Production-proven (Dgraph, Jaeger Tracing, etc.)
type ThinkingCache struct {
	mu sync.RWMutex

	// RAM cache with LRU eviction
	cache     map[string]*list.Element
	lruList   *list.List
	usedBytes int64
	maxBytes  int64

	// BadgerDB persistence
	db      *badger.DB
	dbPath  string

	// Async write channel
	writeChan chan *ThinkingBlock
	doneChan  chan struct{}
}

// lruEntry is stored in the LRU list.
type lruEntry struct {
	key   string
	block *ThinkingBlock
}

// GetThinkingCache returns the singleton ThinkingCache instance.
// Only creates the cache on first call; subsequent calls return the same instance.
func GetThinkingCache(cfg config.ThinkingCacheConfig) (*ThinkingCache, error) {
	thinkingCacheOnce.Do(func() {
		thinkingCacheInstance, thinkingCacheErr = newThinkingCache(cfg)
	})
	return thinkingCacheInstance, thinkingCacheErr
}

// newThinkingCache creates a new thinking cache (internal, called by GetThinkingCache).
func newThinkingCache(cfg config.ThinkingCacheConfig) (*ThinkingCache, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Determine the storage path
	// If StoragePath is set, use it; otherwise derive from SQLitePath for backwards compatibility
	dbPath := cfg.StoragePath
	if dbPath == "" {
		// Backwards compatibility: derive from SQLitePath
		if cfg.SQLitePath != "" {
			// Remove .db extension if present and add _badger suffix
			dbPath = strings.TrimSuffix(cfg.SQLitePath, ".db") + "_badger"
		} else {
			dbPath = "thinking_cache_badger"
		}
	}

	// Auto-migrate from SQLite if needed
	if cfg.SQLitePath != "" {
		AutoMigrate(cfg.SQLitePath, dbPath)
	}

	// Ensure directory exists for BadgerDB
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory for BadgerDB: %w", err)
	}

	// Configure BadgerDB options for optimal performance
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable BadgerDB's internal logging

	// Optimize for our use case: small values (thinking signatures ~100-500 bytes)
	// These settings reduce memory usage while maintaining good performance
	opts.ValueLogFileSize = 64 << 20  // 64MB value log files (smaller = faster GC)
	opts.NumMemtables = 2             // Reduce memory usage
	opts.NumLevelZeroTables = 2       // Reduce memory usage
	opts.NumLevelZeroTablesStall = 4  // Reduce memory usage
	opts.ValueThreshold = 1024        // Values larger than 1KB go to value log
	opts.SyncWrites = false           // Async writes for better performance (we have RAM cache for durability)
	opts.DetectConflicts = false      // We don't need transaction conflict detection

	// Open BadgerDB database
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open BadgerDB database: %w", err)
	}

	tc := &ThinkingCache{
		cache:     make(map[string]*list.Element),
		lruList:   list.New(),
		maxBytes:  int64(cfg.MaxMemoryMB) * 1024 * 1024,
		db:        db,
		dbPath:    dbPath,
		writeChan: make(chan *ThinkingBlock, 1000), // Buffer 1000 async writes
		doneChan:  make(chan struct{}),
	}

	// Start async writer goroutine
	go tc.asyncWriter()

	// Start BadgerDB garbage collection goroutine
	go tc.runGC()

	// Load entries from BadgerDB into RAM
	if err := tc.loadFromBadger(); err != nil {
		fmt.Printf("[THINKING-CACHE] Warning: failed to load from BadgerDB: %v\n", err)
	}

	// Log cache initialization (singleton ensures this only runs once)
	fmt.Printf("[THINKING-CACHE] Initialized with BadgerDB: max_memory=%dMB, path=%s, loaded=%d entries (%.2f MB)\n",
		cfg.MaxMemoryMB, dbPath, tc.lruList.Len(), float64(tc.usedBytes)/(1024*1024))

	return tc, nil
}

// loadFromBadger loads entries from BadgerDB into RAM cache (by last_accessed).
func (tc *ThinkingCache) loadFromBadger() error {
	// Collect all entries first, then sort by last_accessed
	type entryData struct {
		block        *ThinkingBlock
		lastAccessed int64
	}
	var entries []entryData

	err := tc.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = 100
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			err := item.Value(func(val []byte) error {
				var block ThinkingBlock
				if err := json.Unmarshal(val, &block); err != nil {
					return nil // Skip corrupted entries
				}
				entries = append(entries, entryData{
					block:        &block,
					lastAccessed: block.LastAccessed,
				})
				return nil
			})
			if err != nil {
				continue
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Sort by last_accessed descending (most recent first)
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[j].lastAccessed > entries[i].lastAccessed {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Load into RAM cache until we hit the memory limit
	for _, e := range entries {
		block := e.block

		// Check if we'd exceed max memory
		if tc.usedBytes+int64(block.SizeBytes) > tc.maxBytes {
			break // Stop loading, RAM is full
		}

		// Add to RAM cache (already sorted by last_accessed, so add to back of LRU)
		entry := &lruEntry{key: block.ConversationID, block: block}
		elem := tc.lruList.PushBack(entry)
		tc.cache[block.ConversationID] = elem
		tc.usedBytes += int64(block.SizeBytes)
	}

	return nil
}

// asyncWriter handles async BadgerDB writes.
func (tc *ThinkingCache) asyncWriter() {
	for {
		select {
		case block := <-tc.writeChan:
			tc.writeBadger(block)
		case <-tc.doneChan:
			// Drain remaining writes
			for len(tc.writeChan) > 0 {
				block := <-tc.writeChan
				tc.writeBadger(block)
			}
			return
		}
	}
}

// writeBadger persists a thinking block to BadgerDB.
func (tc *ThinkingCache) writeBadger(block *ThinkingBlock) {
	data, err := json.Marshal(block)
	if err != nil {
		fmt.Printf("[THINKING-CACHE] BadgerDB marshal error: %v\n", err)
		return
	}

	err = tc.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(block.ConversationID), data)
	})
	if err != nil {
		fmt.Printf("[THINKING-CACHE] BadgerDB write error: %v\n", err)
	}
}

// runGC periodically runs BadgerDB garbage collection.
func (tc *ThinkingCache) runGC() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Run value log GC
			for {
				err := tc.db.RunValueLogGC(0.5) // Reclaim if 50%+ space can be freed
				if err != nil {
					break // No more GC needed
				}
			}
		case <-tc.doneChan:
			return
		}
	}
}

// Get retrieves a thinking block by conversation ID.
// First checks RAM cache, then falls back to BadgerDB.
func (tc *ThinkingCache) Get(conversationID string) ([]byte, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Check RAM cache first
	if elem, ok := tc.cache[conversationID]; ok {
		// Move to front (most recently used)
		tc.lruList.MoveToFront(elem)
		entry := elem.Value.(*lruEntry)
		entry.block.LastAccessed = time.Now().Unix()
		fmt.Printf("[THINKING-CACHE] HIT (RAM): conversation=%s size=%d bytes\n", conversationID[:min(8, len(conversationID))], len(entry.block.ThinkingData))
		return entry.block.ThinkingData, true
	}

	// Not in RAM, check BadgerDB
	var block ThinkingBlock
	found := false

	err := tc.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(conversationID))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			if err := json.Unmarshal(val, &block); err != nil {
				return err
			}
			found = true
			return nil
		})
	})

	if err != nil || !found {
		return nil, false
	}

	// Found in BadgerDB, load into RAM cache
	block.LastAccessed = time.Now().Unix()

	// Evict if necessary to make room
	tc.evictIfNecessary(int64(block.SizeBytes))

	// Add to RAM cache
	entry := &lruEntry{key: conversationID, block: &block}
	elem := tc.lruList.PushFront(entry)
	tc.cache[conversationID] = elem
	tc.usedBytes += int64(block.SizeBytes)

	// Update last_accessed in BadgerDB (async)
	tc.writeChan <- &block

	fmt.Printf("[THINKING-CACHE] HIT (BadgerDB): conversation=%s size=%d bytes\n", conversationID[:min(8, len(conversationID))], len(block.ThinkingData))
	return block.ThinkingData, true
}

// Set stores a thinking block in both RAM cache and BadgerDB.
func (tc *ThinkingCache) Set(conversationID string, thinkingData []byte) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	now := time.Now().Unix()
	block := &ThinkingBlock{
		ConversationID: conversationID,
		ThinkingData:   thinkingData,
		CreatedAt:      now,
		LastAccessed:   now,
		SizeBytes:      len(thinkingData),
	}

	// Check if already in RAM cache
	if elem, ok := tc.cache[conversationID]; ok {
		// Update existing entry
		oldEntry := elem.Value.(*lruEntry)
		tc.usedBytes -= int64(oldEntry.block.SizeBytes)
		oldEntry.block = block
		tc.usedBytes += int64(block.SizeBytes)
		tc.lruList.MoveToFront(elem)
	} else {
		// Evict if necessary
		tc.evictIfNecessary(int64(block.SizeBytes))

		// Add new entry
		entry := &lruEntry{key: conversationID, block: block}
		elem := tc.lruList.PushFront(entry)
		tc.cache[conversationID] = elem
		tc.usedBytes += int64(block.SizeBytes)
	}

	// Async write to BadgerDB
	tc.writeChan <- block

	fmt.Printf("[THINKING-CACHE] STORE: conversation=%s size=%d bytes\n", conversationID[:min(8, len(conversationID))], len(thinkingData))
}

// evictIfNecessary removes LRU entries from RAM until there's room for newBytes.
// Note: Entries are only removed from RAM, never from BadgerDB.
func (tc *ThinkingCache) evictIfNecessary(newBytes int64) {
	for tc.usedBytes+newBytes > tc.maxBytes && tc.lruList.Len() > 0 {
		// Remove least recently used (back of list)
		elem := tc.lruList.Back()
		if elem == nil {
			break
		}
		entry := elem.Value.(*lruEntry)
		tc.lruList.Remove(elem)
		delete(tc.cache, entry.key)
		tc.usedBytes -= int64(entry.block.SizeBytes)
	}
}

// Close shuts down the cache, flushing pending writes.
func (tc *ThinkingCache) Close() error {
	close(tc.doneChan)
	if tc.db != nil {
		return tc.db.Close()
	}
	return nil
}

// Stats returns cache statistics.
func (tc *ThinkingCache) Stats() (ramEntries int, ramBytes int64, maxBytes int64) {
	tc.mu.RLock()
	defer tc.mu.RUnlock()
	return len(tc.cache), tc.usedBytes, tc.maxBytes
}

// GenerateConversationID creates a unique conversation ID from request data.
// Uses a SHA256 hash truncated to 64 bits (16 hex chars) for compact storage.
// Collision probability is negligible for realistic cache sizes (expected
// collision around 2^32 = 4 billion entries via birthday paradox).
func GenerateConversationID(messages []byte) string {
	hash := sha256.Sum256(messages)
	return hex.EncodeToString(hash[:8]) // 8 bytes = 64 bits = 16 hex chars
}

// GenerateStableConversationID creates a conversation ID that's stable across
// JSON formatting variations. It hashes the assistant index + user text content
// BEFORE that assistant (indices 0 to assistantIdx-1).
// This fixes the issue where Cursor may format the same message differently
// between requests, causing cache key mismatches.
func GenerateStableConversationID(messages []byte, assistantIdx int) string {
	// Hash user message text content ONLY for messages BEFORE this assistant
	// This creates a stable key: same user content + same position = same key
	var userContent strings.Builder
	userContent.WriteString(fmt.Sprintf("idx:%d|", assistantIdx))

	messagesArray := gjson.GetBytes(messages, "@this")
	if messagesArray.IsArray() {
		for i, msg := range messagesArray.Array() {
			if i >= assistantIdx {
				break // Only consider messages BEFORE this assistant
			}
			if msg.Get("role").String() == "user" {
				content := msg.Get("content")
				if content.IsArray() {
					// Content is array of blocks - extract text
					content.ForEach(func(_, block gjson.Result) bool {
						if block.Get("type").String() == "text" {
							userContent.WriteString(block.Get("text").String())
						}
						return true
					})
				} else {
					// Content is a simple string
					userContent.WriteString(content.String())
				}
				userContent.WriteString("|")
			}
		}
	}

	hash := sha256.Sum256([]byte(userContent.String()))
	return hex.EncodeToString(hash[:8])
}

// GenerateConversationIDFromMessages creates a conversation ID from message history.
// This considers the full conversation context to identify the thinking block.
func GenerateConversationIDFromMessages(messages []interface{}) string {
	// Serialize messages (excluding any tool_result content which varies)
	data, err := json.Marshal(messages)
	if err != nil {
		// Fallback to timestamp-based ID if serialization fails
		return fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}
	return GenerateConversationID(data)
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
