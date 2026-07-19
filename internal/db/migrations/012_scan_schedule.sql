-- Global admin-configurable library scan schedule (single-row table).

CREATE TABLE IF NOT EXISTS scan_schedule (
    id               INTEGER      NOT NULL DEFAULT 1,
    enabled          TINYINT(1)   NOT NULL DEFAULT 1,
    interval_seconds INTEGER      NOT NULL DEFAULT 86400,
    updated_at       VARCHAR(32)  NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT chk_scan_schedule_singleton CHECK (id = 1),
    CONSTRAINT chk_scan_schedule_interval CHECK (interval_seconds >= 60)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
