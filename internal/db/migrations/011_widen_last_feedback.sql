-- Fix migration 005: last_feedback stores a JSON feedback-ack object
-- ({"signal","track_id","at"}), which regularly exceeds VARCHAR(32).
-- Widen to LONGTEXT (matches queue_json) for schemas migrated before this fix.
ALTER TABLE listening_sessions MODIFY COLUMN last_feedback LONGTEXT;
