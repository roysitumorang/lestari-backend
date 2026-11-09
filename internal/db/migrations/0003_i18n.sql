-- Notifications and activity logs keep their English text as a translation key plus the values of its
-- {placeholders}, and are translated into the reader's language when listed.
ALTER TABLE notifications ADD COLUMN params JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE activity_logs ADD COLUMN params JSONB NOT NULL DEFAULT '{}'::jsonb;
