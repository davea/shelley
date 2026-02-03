-- Add quiet column to conversations
-- When true, pushover notifications are suppressed for this conversation
ALTER TABLE conversations ADD COLUMN quiet BOOLEAN NOT NULL DEFAULT FALSE;
