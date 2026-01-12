// Package cache provides caching mechanisms for the CLI Proxy API server.
package cache

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dgraph-io/badger/v4"
	_ "github.com/mattn/go-sqlite3"
)

// MigrateSQLiteToBadger migrates existing SQLite cache data to BadgerDB.
// This is useful for users upgrading from the SQLite-based cache to BadgerDB.
// Returns the number of entries migrated and any error encountered.
func MigrateSQLiteToBadger(sqlitePath, badgerPath string) (int, error) {
	// Check if SQLite database exists
	if _, err := os.Stat(sqlitePath); os.IsNotExist(err) {
		return 0, nil // No SQLite database to migrate
	}

	// Check if BadgerDB already has data (avoid duplicate migration)
	if hasBadgerData(badgerPath) {
		fmt.Printf("[MIGRATION] BadgerDB already contains data, skipping migration\n")
		return 0, nil
	}

	fmt.Printf("[MIGRATION] Starting migration from SQLite (%s) to BadgerDB (%s)\n", sqlitePath, badgerPath)

	// Open SQLite database
	sqliteDB, err := sql.Open("sqlite3", sqlitePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open SQLite database: %w", err)
	}
	defer sqliteDB.Close()

	// Ensure BadgerDB directory exists
	if err := os.MkdirAll(badgerPath, 0755); err != nil {
		return 0, fmt.Errorf("failed to create BadgerDB directory: %w", err)
	}

	// Open BadgerDB
	opts := badger.DefaultOptions(badgerPath)
	opts.Logger = nil
	opts.ValueLogFileSize = 64 << 20
	opts.NumMemtables = 2
	opts.NumLevelZeroTables = 2
	opts.NumLevelZeroTablesStall = 4
	opts.ValueThreshold = 1024
	opts.SyncWrites = false
	opts.DetectConflicts = false

	badgerDB, err := badger.Open(opts)
	if err != nil {
		return 0, fmt.Errorf("failed to open BadgerDB: %w", err)
	}
	defer badgerDB.Close()

	// Query all entries from SQLite
	rows, err := sqliteDB.Query(`
		SELECT conversation_id, thinking_block, created_at, last_accessed
		FROM thinking_cache
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to query SQLite: %w", err)
	}
	defer rows.Close()

	// Migrate entries
	migrated := 0
	wb := badgerDB.NewWriteBatch()
	defer wb.Cancel()

	for rows.Next() {
		var conversationID string
		var thinkingData []byte
		var createdAt, lastAccessed int64

		if err := rows.Scan(&conversationID, &thinkingData, &createdAt, &lastAccessed); err != nil {
			fmt.Printf("[MIGRATION] Warning: failed to scan row: %v\n", err)
			continue
		}

		block := &ThinkingBlock{
			ConversationID: conversationID,
			ThinkingData:   thinkingData,
			CreatedAt:      createdAt,
			LastAccessed:   lastAccessed,
			SizeBytes:      len(thinkingData),
		}

		data, err := json.Marshal(block)
		if err != nil {
			fmt.Printf("[MIGRATION] Warning: failed to marshal block: %v\n", err)
			continue
		}

		if err := wb.Set([]byte(conversationID), data); err != nil {
			fmt.Printf("[MIGRATION] Warning: failed to write to BadgerDB: %v\n", err)
			continue
		}

		migrated++

		// Flush batch periodically to avoid memory issues
		if migrated%1000 == 0 {
			if err := wb.Flush(); err != nil {
				return migrated, fmt.Errorf("failed to flush batch: %w", err)
			}
			wb = badgerDB.NewWriteBatch()
		}
	}

	// Final flush
	if err := wb.Flush(); err != nil {
		return migrated, fmt.Errorf("failed to flush final batch: %w", err)
	}

	fmt.Printf("[MIGRATION] Successfully migrated %d entries from SQLite to BadgerDB\n", migrated)

	// Optionally rename the old SQLite file to indicate it's been migrated
	backupPath := sqlitePath + ".migrated"
	if err := os.Rename(sqlitePath, backupPath); err != nil {
		fmt.Printf("[MIGRATION] Warning: could not rename old SQLite file: %v\n", err)
	} else {
		fmt.Printf("[MIGRATION] Renamed old SQLite file to %s\n", backupPath)
	}

	return migrated, nil
}

// hasBadgerData checks if the BadgerDB directory contains data.
func hasBadgerData(badgerPath string) bool {
	// Check if the directory exists and has files
	entries, err := os.ReadDir(badgerPath)
	if err != nil {
		return false
	}

	// Look for BadgerDB files (MANIFEST, SSTables, etc.)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".sst") ||
			strings.HasPrefix(name, "MANIFEST") ||
			strings.HasSuffix(name, ".vlog") {
			return true
		}
	}

	return false
}

// AutoMigrate performs automatic migration if needed.
// It checks for an existing SQLite database and migrates to BadgerDB if necessary.
// This should be called during cache initialization.
func AutoMigrate(sqlitePath, badgerPath string) {
	if sqlitePath == "" {
		return
	}

	// Check if SQLite database exists
	if _, err := os.Stat(sqlitePath); os.IsNotExist(err) {
		return // No SQLite database to migrate
	}

	// Perform migration
	migrated, err := MigrateSQLiteToBadger(sqlitePath, badgerPath)
	if err != nil {
		fmt.Printf("[MIGRATION] Error during migration: %v\n", err)
		return
	}

	if migrated > 0 {
		fmt.Printf("[MIGRATION] Auto-migration complete: %d entries migrated\n", migrated)
	}
}
