-- Phase 4+: admin flag for local users (bootstrap via ADMIN_USERNAME / ADMIN_PASSWORD).

ALTER TABLE users ADD COLUMN IF NOT EXISTS is_admin INTEGER NOT NULL DEFAULT 0;
