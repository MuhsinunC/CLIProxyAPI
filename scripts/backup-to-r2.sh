#!/bin/bash
# scripts/backup-to-r2.sh - Backup thinking cache to R2
# Only uploads if cache size has changed since last backup
set -e

CACHE_DIR="/data/cache/thinking_badger"
BACKUP_FILE="/tmp/cache-backup-$(date +%Y%m%d-%H%M%S).tar.gz"
SIZE_FILE="/data/cache/.last_backup_size"

# Get current cache size
CURRENT_SIZE=$(du -sb "$CACHE_DIR" 2>/dev/null | cut -f1 || echo "0")

# Get last backup size (if recorded)
LAST_SIZE="0"
if [ -f "$SIZE_FILE" ]; then
  LAST_SIZE=$(cat "$SIZE_FILE")
fi

echo "Current cache size: $CURRENT_SIZE bytes"
echo "Last backup size:   $LAST_SIZE bytes"

# Skip if sizes match (no changes)
if [ "$CURRENT_SIZE" = "$LAST_SIZE" ]; then
  echo "Cache unchanged since last backup. Skipping."
  exit 0
fi

echo "Cache has changed. Creating backup..."
tar -czf "$BACKUP_FILE" -C /data/cache thinking_badger

echo "Uploading to R2..."
rclone copy "$BACKUP_FILE" r2:cliproxy-backup/

# Keep latest as cache-backup.tar.gz for easy restore
rclone copyto "$BACKUP_FILE" r2:cliproxy-backup/cache-backup.tar.gz

# Record current size for next comparison
echo "$CURRENT_SIZE" > "$SIZE_FILE"

# Cleanup old backups (keep last 7)
rclone delete r2:cliproxy-backup/ --min-age 7d --include "cache-backup-*.tar.gz"

# Get backup size for logging
BACKUP_SIZE=$(du -h "$BACKUP_FILE" 2>/dev/null | cut -f1 || echo "N/A")
rm "$BACKUP_FILE"
echo "Backup complete! Uploaded $BACKUP_SIZE"
