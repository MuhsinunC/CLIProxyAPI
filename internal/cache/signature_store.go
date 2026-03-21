package cache

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	log "github.com/sirupsen/logrus"
)

// Package-level singleton state for the persistent signature store.
var (
	signatureStore   *badger.DB
	storeWriteChan   chan storeEntry
	storeDoneChan    chan struct{}
	storeWG          sync.WaitGroup
	storeInitOnce    sync.Once
	storeInitialized atomic.Bool
)

// storeEntry represents a signature entry to be written to BadgerDB.
type storeEntry struct {
	GroupKey  string
	TextHash  string
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
			WithValueLogFileSize(64 << 20).
			WithNumMemtables(2).
			WithNumLevelZeroTables(2).
			WithNumLevelZeroTablesStall(4).
			WithValueThreshold(1024).
			WithSyncWrites(false).
			WithDetectConflicts(false).
			WithLogger(nil)

		db, err := badger.Open(opts)
		if err != nil {
			initErr = fmt.Errorf("failed to open signature store at %s: %w", path, err)
			return
		}

		signatureStore = db
		storeWriteChan = make(chan storeEntry, 1000)
		storeDoneChan = make(chan struct{})

		loaded := storeLoadAll()
		if loaded > 0 {
			log.Infof("[SIGNATURE-STORE] Loaded %d signatures from disk", loaded)
		}

		storeWG.Add(2)
		go func() { defer storeWG.Done(); asyncStoreWriter() }()
		go func() { defer storeWG.Done(); runStoreGC() }()

		storeInitialized.Store(true)
		log.Infof("[SIGNATURE-STORE] Initialized at %s", path)
	})
	return initErr
}

// CloseSignatureStore gracefully shuts down the persistent store.
// Drains pending writes, waits for background goroutines, and closes BadgerDB.
func CloseSignatureStore() {
	if !storeInitialized.Load() || signatureStore == nil {
		return
	}
	storeInitialized.Store(false)

	// Signal background goroutines to stop and wait for them to finish
	close(storeDoneChan)
	storeWG.Wait()

	if err := signatureStore.Close(); err != nil {
		log.Warnf("[SIGNATURE-STORE] Error closing: %v", err)
	}
	log.Info("[SIGNATURE-STORE] Closed")
}

// storeGet retrieves a signature from BadgerDB by groupKey and textHash.
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
		opts.PrefetchValues = false
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

			slashIdx := strings.IndexByte(key, '/')
			if slashIdx < 0 {
				continue
			}
			groupKey := key[:slashIdx]
			textHash := key[slashIdx+1:]

			err := item.Value(func(val []byte) error {
				parts := strings.SplitN(string(val), "\x00", 2)
				if len(parts) == 0 || parts[0] == "" {
					return nil
				}
				signature := parts[0]

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
// to the in-memory sync.Map on hit.
func storeGetAndPromote(groupKey, textHash string) string {
	if !storeInitialized.Load() {
		return ""
	}
	sig, ok := storeGet(groupKey, textHash)
	if !ok {
		return ""
	}
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
			if signatureStore == nil {
				return
			}
			for {
				err := signatureStore.RunValueLogGC(0.5)
				if err != nil {
					break
				}
			}
		case <-storeDoneChan:
			return
		}
	}
}
