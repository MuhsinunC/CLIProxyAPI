#!/bin/bash
set -e

# Create data directories if they don't exist
mkdir -p /data/auth /data/cache /data/logs

# Configure rclone for R2 (if credentials exist)
if [ -n "$R2_ACCESS_KEY" ]; then
  mkdir -p ~/.config/rclone
  cat > ~/.config/rclone/rclone.conf << EOF
[r2]
type = s3
provider = Cloudflare
access_key_id = ${R2_ACCESS_KEY}
secret_access_key = ${R2_SECRET_KEY}
endpoint = https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com
acl = private
EOF
fi

# Generate config.yaml from environment variables
cat > /data/config.yaml << EOF
port: 8317
host: "0.0.0.0"
auth-dir: "/data/auth"
debug: false
logging-to-file: true
logs-dir: "/data/logs"

api-keys:
  - "${PROXY_API_KEY}"

thinking-cache:
  enabled: true
  max-memory-mb: 128
  storage-path: "/data/cache/thinking_badger"
EOF

# Restore from R2 backup if cache is empty and backup exists
if [ ! -d "/data/cache/thinking_badger" ] && [ -n "$R2_ACCESS_KEY" ]; then
  echo "Checking for R2 backup to restore..."
  if rclone ls r2:cliproxy-backup/cache-backup.tar.gz 2>/dev/null; then
    echo "Restoring thinking cache from R2 backup..."
    rclone copy r2:cliproxy-backup/cache-backup.tar.gz /tmp/
    tar -xzf /tmp/cache-backup.tar.gz -C /data/cache/
    rm /tmp/cache-backup.tar.gz
    echo "Cache restored successfully"
  fi
fi

# Start the server
exec /app/cli-proxy-api --config /data/config.yaml
