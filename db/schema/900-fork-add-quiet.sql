-- Add quiet column to conversations for per-conversation notification suppression.
-- Also ensure notification_channels table exists (handles upgrades from older fork
-- installs where migration 015 was the quiet column, not the upstream notification
-- channels table).

CREATE TABLE IF NOT EXISTS notification_channels (
    channel_id TEXT PRIMARY KEY,
    channel_type TEXT NOT NULL,
    display_name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    config TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

ALTER TABLE conversations ADD COLUMN quiet BOOLEAN NOT NULL DEFAULT FALSE;
