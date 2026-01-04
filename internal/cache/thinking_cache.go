// Package cache provides caching mechanisms for the CLI Proxy API server.
// Currently includes thinking block caching for Claude extended thinking tool loops.
package cache

import (
	"container/list"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"

	_ "github.com/mattn/go-sqlite3"
)

// ThinkingBlock represents a cached Claude thinking block with signature.
type ThinkingBlock struct {
	ConversationID string `json:"conversation_id"`
	ThinkingData   []byte `json:"thinking_data"` // Raw Claude-format thinking block JSON
	CreatedAt      int64  `json:"created_at"`
	LastAccessed   int64  `json:"last_accessed"`
	SizeBytes      int    `json:"size_bytes"`
}

// ThinkingCache provides RAM + SQLite caching for Claude thinking blocks.
// RAM is used for fast access with LRU eviction; SQLite provides persistence.
type ThinkingCache struct {
	mu sync.RWMutex

	// RAM cache with LRU eviction
	cache     map[string]*list.Element
	lruList   *list.List
	usedBytes int64
	maxBytes  int64

	// SQLite persistence
	db         *sql.DB
	sqlitePath string

	// Async write channel
	writeChan chan *ThinkingBlock
	doneChan  chan struct{}
}

// lruEntry is stored in the LRU list.
type lruEntry struct {
	key   string
	block *ThinkingBlock
}

// NewThinkingCache creates a new thinking cache with the given configuration.
// It opens/creates the SQLite database and loads existing entries into RAM.
func NewThinkingCache(cfg config.ThinkingCacheConfig) (*ThinkingCache, error) {
	if !cfg.Enabled {
		return nil, nil
	}

	// Ensure directory exists for SQLite file
	dir := filepath.Dir(cfg.SQLitePath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory for SQLite: %w", err)
		}
	}

	// Open SQLite database
	db, err := sql.Open("sqlite3", cfg.SQLitePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite database: %w", err)
	}

	// Create table if not exists
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS thinking_cache (
			conversation_id TEXT PRIMARY KEY,
			thinking_block  BLOB NOT NULL,
			created_at      INTEGER NOT NULL,
			last_accessed   INTEGER NOT NULL
		);
		CREATE INDEX IF NOT EXISTS idx_last_accessed ON thinking_cache(last_accessed DESC);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create thinking_cache table: %w", err)
	}

	tc := &ThinkingCache{
		cache:      make(map[string]*list.Element),
		lruList:    list.New(),
		maxBytes:   int64(cfg.MaxMemoryMB) * 1024 * 1024,
		db:         db,
		sqlitePath: cfg.SQLitePath,
		writeChan:  make(chan *ThinkingBlock, 1000), // Buffer 1000 async writes
		doneChan:   make(chan struct{}),
	}

	// Start async writer goroutine
	go tc.asyncWriter()

	// Load entries from SQLite into RAM
	if err := tc.loadFromSQLite(); err != nil {
		fmt.Printf("[THINKING-CACHE] Warning: failed to load from SQLite: %v\n", err)
	}

	fmt.Printf("[THINKING-CACHE] Initialized: max_memory=%dMB, sqlite=%s, loaded=%d entries (%.2f MB)\n",
		cfg.MaxMemoryMB, cfg.SQLitePath, tc.lruList.Len(), float64(tc.usedBytes)/(1024*1024))

	return tc, nil
}

// loadFromSQLite loads top entries from SQLite into RAM cache (by last_accessed).
func (tc *ThinkingCache) loadFromSQLite() error {
	rows, err := tc.db.Query(`
		SELECT conversation_id, thinking_block, created_at, last_accessed
		FROM thinking_cache
		ORDER BY last_accessed DESC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var block ThinkingBlock
		var thinkingData []byte
		if err := rows.Scan(&block.ConversationID, &thinkingData, &block.CreatedAt, &block.LastAccessed); err != nil {
			continue
		}
		block.ThinkingData = thinkingData
		block.SizeBytes = len(thinkingData)

		// Check if we'd exceed max memory
		if tc.usedBytes+int64(block.SizeBytes) > tc.maxBytes {
			break // Stop loading, RAM is full
		}

		// Add to RAM cache (already sorted by last_accessed, so add to back of LRU)
		entry := &lruEntry{key: block.ConversationID, block: &block}
		elem := tc.lruList.PushBack(entry)
		tc.cache[block.ConversationID] = elem
		tc.usedBytes += int64(block.SizeBytes)
	}

	return nil
}

// asyncWriter handles async SQLite writes.
func (tc *ThinkingCache) asyncWriter() {
	for {
		select {
		case block := <-tc.writeChan:
			tc.writeSQLite(block)
		case <-tc.doneChan:
			// Drain remaining writes
			for len(tc.writeChan) > 0 {
				block := <-tc.writeChan
				tc.writeSQLite(block)
			}
			return
		}
	}
}

// writeSQLite persists a thinking block to SQLite.
func (tc *ThinkingCache) writeSQLite(block *ThinkingBlock) {
	_, err := tc.db.Exec(`
		INSERT OR REPLACE INTO thinking_cache (conversation_id, thinking_block, created_at, last_accessed)
		VALUES (?, ?, ?, ?)
	`, block.ConversationID, block.ThinkingData, block.CreatedAt, block.LastAccessed)
	if err != nil {
		fmt.Printf("[THINKING-CACHE] SQLite write error: %v\n", err)
	}
}

// Get retrieves a thinking block by conversation ID.
// First checks RAM cache, then falls back to SQLite.
func (tc *ThinkingCache) Get(conversationID string) ([]byte, bool) {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	// Check RAM cache first
	if elem, ok := tc.cache[conversationID]; ok {
		// Move to front (most recently used)
		tc.lruList.MoveToFront(elem)
		entry := elem.Value.(*lruEntry)
		entry.block.LastAccessed = time.Now().Unix()
		fmt.Printf("[THINKING-CACHE] HIT (RAM): conversation=%s size=%d bytes\n", conversationID[:8], len(entry.block.ThinkingData))
		return entry.block.ThinkingData, true
	}

	// Not in RAM, check SQLite
	var thinkingData []byte
	var createdAt, lastAccessed int64
	err := tc.db.QueryRow(`
		SELECT thinking_block, created_at, last_accessed
		FROM thinking_cache
		WHERE conversation_id = ?
	`, conversationID).Scan(&thinkingData, &createdAt, &lastAccessed)

	if err != nil {
		return nil, false
	}

	// Found in SQLite, load into RAM cache
	block := &ThinkingBlock{
		ConversationID: conversationID,
		ThinkingData:   thinkingData,
		CreatedAt:      createdAt,
		LastAccessed:   time.Now().Unix(),
		SizeBytes:      len(thinkingData),
	}

	// Evict if necessary to make room
	tc.evictIfNecessary(int64(block.SizeBytes))

	// Add to RAM cache
	entry := &lruEntry{key: conversationID, block: block}
	elem := tc.lruList.PushFront(entry)
	tc.cache[conversationID] = elem
	tc.usedBytes += int64(block.SizeBytes)

	// Update last_accessed in SQLite (async)
	tc.writeChan <- block

	fmt.Printf("[THINKING-CACHE] HIT (SQLite): conversation=%s size=%d bytes\n", conversationID[:8], len(thinkingData))
	return thinkingData, true
}

// Set stores a thinking block in both RAM cache and SQLite.
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

	// Async write to SQLite
	tc.writeChan <- block

	fmt.Printf("[THINKING-CACHE] STORE: conversation=%s size=%d bytes\n", conversationID[:8], len(thinkingData))
}

// evictIfNecessary removes LRU entries from RAM until there's room for newBytes.
// Note: Entries are only removed from RAM, never from SQLite.
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
