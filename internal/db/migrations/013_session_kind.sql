-- Session kind: radio (affinity station) vs album (finite album playthrough).
ALTER TABLE listening_sessions
  ADD COLUMN kind VARCHAR(16) NOT NULL DEFAULT 'radio' AFTER status;

CREATE INDEX idx_listening_sessions_user_kind ON listening_sessions (user_id, kind, updated_at);
