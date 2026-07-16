// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-16

package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
)

// migrateSqlite is a one-shot importer for the legacy SQLite catalog
// (data/latenttone.db) into the MariaDB catalog. It shells out to the
// `sqlite3` CLI to read the source file rather than linking a SQLite Go
// driver, so it adds no build-time or runtime Go dependency — the app's
// go.mod stays MariaDB-only per the hard cutover.
//
// Usage (see README "Importing an existing SQLite catalog"):
//
//	latenttone migrate-sqlite --source /data/latenttone.db --config /config/scanner.yaml --yes
//
// The importer is idempotent: it TRUNCATEs each destination table (in a
// single connection with FOREIGN_KEY_CHECKS=0, so table order does not
// matter for referential integrity) and re-imports from source, preserving
// primary keys so LanceDB vector ids and playlist/session references stay
// valid. schema_migrations is never touched — it reflects the MariaDB
// migration state, not the SQLite one.
//
// SQLite's `COLLATE NOCASE` (ASCII case-fold only) is looser than MariaDB's
// `utf8mb4_unicode_ci` (case- AND accent-insensitive), so a small number of
// SQLite rows that were distinct under NOCASE — e.g. two artists named
// "Live" and "Lïve" — can collide under a MariaDB unique key. For each
// table with a natural unique key (artists.name, genres.name,
// albums(artist_id,title), tracks.path, users.username) the importer
// inserts row by row, detects such collisions, and remaps the losing row's
// id to the row that won the race (lowest source id, inserted first) before
// importing dependent tables — so e.g. every track_artists row that pointed
// at the "Lïve" duplicate now points at the surviving "Live" row instead of
// erroring out.
func migrateSqlite(args []string) int {
	fs := flag.NewFlagSet("migrate-sqlite", flag.ExitOnError)
	source := fs.String("source", "/data/latenttone.db", "path to the source SQLite catalog file")
	cfgPath := fs.String("config", "/config/scanner.yaml", "path to scanner.yaml (for database_dsn)")
	dsnFlag := fs.String("dsn", "", "MariaDB DSN override (defaults to scanner.yaml / DATABASE_DSN)")
	sqlite3Path := fs.String("sqlite3", "sqlite3", "path to the sqlite3 CLI binary")
	batchSize := fs.Int("batch-size", 500, "rows per INSERT statement for tables without a natural unique key")
	yes := fs.Bool("yes", false, "actually perform the TRUNCATE + import (default is a dry run)")
	_ = fs.Parse(args)

	if _, err := os.Stat(*source); err != nil {
		log.Printf("source sqlite file: %v", err)
		return 1
	}
	if _, err := exec.LookPath(*sqlite3Path); err != nil {
		log.Printf("sqlite3 CLI not found (%s): %v — install sqlite3 or pass -sqlite3=/path/to/sqlite3", *sqlite3Path, err)
		return 1
	}

	dsn := strings.TrimSpace(*dsnFlag)
	if dsn == "" {
		cfg, err := config.Load(*cfgPath)
		if err != nil {
			log.Println(err)
			return 1
		}
		dsn = cfg.DatabaseDSN
	}

	catalog, err := db.Open(dsn)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer catalog.Close()

	imp := &sqliteImporter{
		sqlite3Path: *sqlite3Path,
		sourcePath:  *source,
		batchSize:   *batchSize,
		dryRun:      !*yes,
	}
	if err := imp.run(catalog.SQL); err != nil {
		log.Println(err)
		return 1
	}
	return 0
}

// migrateColKind is the destination MariaDB parameter type used when
// converting a raw sqlite3 JSON value for a bind parameter.
type migrateColKind int

const (
	kindString migrateColKind = iota
	kindInt
	kindFloat
	kindBlobHex // source column read via hex(col); decoded back to []byte
)

type migrateCol struct {
	name string
	kind migrateColKind
	// refTable, if set (kindInt only), is the migrateTable.name whose id
	// remap (built while importing that table) should be applied to this
	// column's values before insert.
	refTable string
}

// migrateTable describes one source→destination table copy, in FK-safe
// order (used for operator-readable logging; correctness relies on
// FOREIGN_KEY_CHECKS=0 for the duration of the import, not on this order).
type migrateTable struct {
	name          string
	cols          []migrateCol
	autoIncrement bool // reset AUTO_INCREMENT to MAX(id)+1 after explicit-id insert
	// naturalKeyCols, if non-empty, names a unique key (besides id) that can
	// collide under MariaDB's collation even though the source rows were
	// distinct in SQLite. Triggers row-by-row insert + dedup-by-remap
	// instead of the faster batched INSERT IGNORE path.
	naturalKeyCols []string
}

func c(name string) migrateCol     { return migrateCol{name: name, kind: kindString} }
func ci(name string) migrateCol    { return migrateCol{name: name, kind: kindInt} }
func cf(name string) migrateCol    { return migrateCol{name: name, kind: kindFloat} }
func cblob(name string) migrateCol { return migrateCol{name: name, kind: kindBlobHex} }
func cref(name, refTable string) migrateCol {
	return migrateCol{name: name, kind: kindInt, refTable: refTable}
}

var migrateTables = []migrateTable{
	{name: "artists", autoIncrement: true, naturalKeyCols: []string{"name"},
		cols: []migrateCol{ci("id"), c("name"), c("name_sort"), c("mbid"), c("created_at"), c("updated_at")}},
	{name: "genres", autoIncrement: true, naturalKeyCols: []string{"name"},
		cols: []migrateCol{ci("id"), c("name"), cref("parent_id", "genres")}},
	{name: "albums", autoIncrement: true, naturalKeyCols: []string{"artist_id", "title"},
		cols: []migrateCol{ci("id"), cref("artist_id", "artists"), c("title"), c("title_sort"), ci("year"), c("mbid"), c("cover_path"), c("created_at"), c("updated_at")}},
	{name: "tracks", autoIncrement: true, naturalKeyCols: []string{"path"},
		cols: []migrateCol{ci("id"), cref("album_id", "albums"), c("path"), c("path_hash"), ci("file_mtime"), ci("file_size"), c("title"), ci("track_number"), ci("disc_number"), ci("duration_ms"), ci("bitrate_kbps"), ci("sample_rate_hz"), ci("channels"), c("format"), ci("year"), c("comment"), c("mbid"), c("catalogued_at"), c("updated_at"), c("missing_at")}},
	{name: "track_artists", cols: []migrateCol{cref("track_id", "tracks"), cref("artist_id", "artists"), c("role"), ci("position")}},
	{name: "track_genres", cols: []migrateCol{cref("track_id", "tracks"), cref("genre_id", "genres"), c("source")}},
	{name: "track_features", cols: []migrateCol{cref("track_id", "tracks"), c("extractor"), c("model_version"), c("features_json"), ci("vector_dim"), c("created_at"), c("updated_at")}},
	{name: "track_vectors", cols: []migrateCol{cref("track_id", "tracks"), c("status"), c("extractor_set"), c("model_versions"), c("lancedb_id"), ci("vector_dim"), cblob("embedding_blob"), c("error_message"), ci("audio_mtime_at_run"), c("created_at"), c("updated_at")}},
	{name: "users", autoIncrement: true, naturalKeyCols: []string{"username"},
		cols: []migrateCol{ci("id"), c("username"), c("password_hash"), c("created_at"), c("updated_at"), ci("is_admin")}},
	{name: "auth_sessions", cols: []migrateCol{c("id"), cref("user_id", "users"), c("created_at"), c("expires_at"), c("last_seen_at")}},
	{name: "user_radio_prefs", cols: []migrateCol{cref("user_id", "users"), ci("radio_bridge"), ci("artist_cooldown"), ci("query_jitter"), ci("artist_penalty"), ci("bounded_random"), cf("jitter_alpha"), c("updated_at")}},
	{name: "user_stream_prefs", cols: []migrateCol{cref("user_id", "users"), c("stream_format"), ci("bitrate_kbps"), c("updated_at")}},
	{name: "playlists", autoIncrement: true, cols: []migrateCol{ci("id"), c("name"), cref("seed_track_id", "tracks"), cref("user_id", "users"), c("kind"), ci("length"), c("created_at"), c("updated_at")}},
	{name: "playlist_tracks", cols: []migrateCol{ci("playlist_id"), ci("position"), cref("track_id", "tracks"), cf("score")}},
	{name: "track_feedback", autoIncrement: true, cols: []migrateCol{ci("id"), cref("user_id", "users"), cref("track_id", "tracks"), c("signal"), c("session_id"), c("created_at")}},
	{name: "playback_events", autoIncrement: true, cols: []migrateCol{ci("id"), cref("user_id", "users"), cref("track_id", "tracks"), c("session_id"), c("started_at"), c("ended_at"), ci("listened_ms"), ci("completed"), ci("skipped"), ci("skip_within_ms")}},
	{name: "user_track_affinity", cols: []migrateCol{cref("user_id", "users"), cref("track_id", "tracks"), cf("score"), c("updated_at")}},
	{name: "user_track_skips", cols: []migrateCol{cref("user_id", "users"), cref("track_id", "tracks"), c("scope"), c("session_key"), c("created_at")}},
	{name: "listening_sessions", cols: []migrateCol{c("id"), cref("user_id", "users"), cref("seed_track_id", "tracks"), c("status"), cref("now_playing_id", "tracks"), c("queue_json"), c("last_feedback"), c("error_message"), c("created_at"), c("updated_at")}},
	{name: "scan_runs", autoIncrement: true, cols: []migrateCol{ci("id"), c("started_at"), c("finished_at"), c("trigger"), ci("files_seen"), ci("files_upserted"), ci("files_missing"), c("status"), c("error_message")}},
	{name: "embed_runs", autoIncrement: true, cols: []migrateCol{ci("id"), c("started_at"), c("finished_at"), c("trigger"), c("sample_mode"), ci("max_tracks"), ci("tracks_claimed"), ci("tracks_ok"), ci("tracks_error"), c("status"), c("error_message")}},
}

// quoteIdent backtick-quotes a column name for MariaDB, since a few catalog
// columns (`signal`, `trigger`) are reserved words there (though not in
// SQLite, where the unquoted SELECT in buildSelect is fine as-is).
func quoteIdent(name string) string {
	return "`" + name + "`"
}

func colIndex(t migrateTable, name string) int {
	for i, col := range t.cols {
		if col.name == name {
			return i
		}
	}
	return -1
}

type sqliteImporter struct {
	sqlite3Path string
	sourcePath  string
	batchSize   int
	dryRun      bool
}

func (imp *sqliteImporter) run(sqlDB *sql.DB) error {
	ctx := context.Background()
	conn, err := sqlDB.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	// table -> source id -> surviving target id (only populated when a
	// natural-key collision remapped a row onto an earlier one).
	remaps := map[string]map[int64]int64{}
	// table -> source id -> true for rows dropped outright (e.g. a value
	// too wide for its MariaDB column — a pre-existing data-quality defect
	// in the SQLite source, not a collation issue). Any row in another
	// table that references a skipped id is skipped too.
	skipped := map[string]map[int64]bool{}
	markSkipped := func(table string, id int64) {
		if skipped[table] == nil {
			skipped[table] = map[int64]bool{}
		}
		skipped[table][id] = true
	}
	rowSkippedByRef := func(t migrateTable, row []interface{}) bool {
		for i, col := range t.cols {
			if col.refTable == "" || row[i] == nil {
				continue
			}
			iv, ok := row[i].(int64)
			if !ok {
				continue
			}
			if skipped[col.refTable] != nil && skipped[col.refTable][iv] {
				return true
			}
		}
		return false
	}

	if imp.dryRun {
		log.Printf("dry run (pass -yes to actually TRUNCATE + import): counting source rows only")
	} else {
		if _, err := conn.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS=0"); err != nil {
			return fmt.Errorf("disable FK checks: %w", err)
		}
		defer func() {
			if _, err := conn.ExecContext(context.Background(), "SET FOREIGN_KEY_CHECKS=1"); err != nil {
				log.Printf("re-enable FK checks: %v", err)
			}
		}()
	}

	type result struct {
		table            string
		srcCount         int
		dstCount         int
		dedupedDuplicate int
		droppedBadData   int
	}
	var results []result

	for _, t := range migrateTables {
		rows, err := imp.readSourceRows(t)
		if err != nil {
			return fmt.Errorf("read %s from sqlite: %w", t.name, err)
		}
		if imp.dryRun {
			results = append(results, result{table: t.name, srcCount: len(rows)})
			continue
		}

		if _, err := conn.ExecContext(ctx, "TRUNCATE TABLE "+t.name); err != nil {
			return fmt.Errorf("truncate %s: %w", t.name, err)
		}

		// One transaction per table: row-by-row natural-key inserts still
		// need a round trip each (to see the duplicate-key error and react),
		// but batching the *commits* into one per table — instead of MariaDB's
		// default autocommit-with-fsync per statement — is what makes an
		// otherwise-fast set of single-row INSERTs actually fast.
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", t.name, err)
		}
		deduped := 0
		dropped := 0
		if len(t.naturalKeyCols) > 0 {
			idIdx := colIndex(t, "id")
			naturalIdx := make([]int, len(t.naturalKeyCols))
			for i, name := range t.naturalKeyCols {
				naturalIdx[i] = colIndex(t, name)
			}
			for _, row := range rows {
				applyRemap(t, row, remaps)
				sourceID, _ := row[idIdx].(int64)
				if rowSkippedByRef(t, row) {
					dropped++
					markSkipped(t.name, sourceID)
					continue
				}
				targetID, dup, err := imp.insertWithDedup(ctx, tx, t, row, naturalIdx)
				if err != nil {
					var myErr *mysqldriver.MySQLError
					if errors.As(err, &myErr) && myErr.Number == 1406 {
						// Column too narrow for the value — a pre-existing
						// data-quality defect in the SQLite source (seen with
						// a corrupt concatenated genre tag), not something
						// this import can safely repair. Drop the row and any
						// rows elsewhere that reference it.
						log.Printf("WARNING %s: source id=%d dropped (value too long for its MariaDB column — likely corrupt source data): %v",
							t.name, sourceID, err)
						dropped++
						markSkipped(t.name, sourceID)
						continue
					}
					_ = tx.Rollback()
					return fmt.Errorf("insert %s (source id=%d): %w", t.name, sourceID, err)
				}
				if dup {
					deduped++
					if remaps[t.name] == nil {
						remaps[t.name] = map[int64]int64{}
					}
					remaps[t.name][sourceID] = targetID
					log.Printf("%s: source id=%d collided with an existing row under MariaDB collation; remapped to id=%d",
						t.name, sourceID, targetID)
				}
			}
		} else {
			kept := make([][]interface{}, 0, len(rows))
			for _, row := range rows {
				applyRemap(t, row, remaps)
				if rowSkippedByRef(t, row) {
					dropped++
					continue
				}
				kept = append(kept, row)
			}
			if err := imp.insertRowsIgnore(ctx, tx, t, kept); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("insert %s: %w", t.name, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", t.name, err)
		}

		if t.autoIncrement {
			if err := resetAutoIncrement(ctx, conn, t.name); err != nil {
				return fmt.Errorf("reset auto_increment for %s: %w", t.name, err)
			}
		}

		var dstCount int
		if err := conn.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+t.name).Scan(&dstCount); err != nil {
			return fmt.Errorf("count %s: %w", t.name, err)
		}
		results = append(results, result{table: t.name, srcCount: len(rows), dstCount: dstCount, dedupedDuplicate: deduped, droppedBadData: dropped})
		note := ""
		if deduped > 0 {
			note += fmt.Sprintf(" (%d collation duplicate(s) merged)", deduped)
		}
		if dropped > 0 {
			note += fmt.Sprintf(" (%d row(s) dropped — bad source data)", dropped)
		}
		log.Printf("%-22s source=%-8d imported=%-8d%s", t.name, len(rows), dstCount, note)
	}

	log.Println("---")
	mismatches := 0
	for _, r := range results {
		if imp.dryRun {
			log.Printf("%-22s source=%d row(s) (dry run — no write)", r.table, r.srcCount)
			continue
		}
		wantCount := r.srcCount - r.dedupedDuplicate - r.droppedBadData
		status := "ok"
		if wantCount != r.dstCount {
			status = "MISMATCH"
			mismatches++
		}
		extra := ""
		if r.droppedBadData > 0 {
			extra = fmt.Sprintf(" (%d dropped — bad source data)", r.droppedBadData)
		}
		log.Printf("%-22s source=%-8d imported=%-8d %s%s", r.table, r.srcCount, r.dstCount, status, extra)
	}
	if imp.dryRun {
		log.Println("dry run complete; re-run with -yes to import")
		return nil
	}
	if mismatches > 0 {
		return fmt.Errorf("%d table(s) had row-count mismatches after import", mismatches)
	}
	log.Println("import complete; row counts verified. LanceDB vector blobs are untouched " +
		"(only track_vectors status rows moved); if tracks show status=pending/error, start " +
		"(or wait for) the embed service to catch up.")
	return nil
}

// applyRemap rewrites any refTable-tagged column in row using remaps built
// so far (including earlier rows of the same table, for self-references
// such as genres.parent_id).
func applyRemap(t migrateTable, row []interface{}, remaps map[string]map[int64]int64) {
	for i, col := range t.cols {
		if col.refTable == "" || row[i] == nil {
			continue
		}
		rm := remaps[col.refTable]
		if rm == nil {
			continue
		}
		iv, ok := row[i].(int64)
		if !ok {
			continue
		}
		if nv, ok2 := rm[iv]; ok2 {
			row[i] = nv
		}
	}
}

// sqlRunner is the subset of *sql.Conn / *sql.Tx used below, so the insert
// helpers work whether called directly on the connection or inside a
// transaction.
type sqlRunner interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// insertWithDedup inserts one row; if it collides on the table's natural
// key (a MySQL 1062 duplicate-entry error), it looks up the id of the
// row that already holds that key and returns it with dup=true instead of
// failing the import.
func (imp *sqliteImporter) insertWithDedup(ctx context.Context, runner sqlRunner, t migrateTable, row []interface{}, naturalIdx []int) (targetID int64, dup bool, err error) {
	colNames := make([]string, len(t.cols))
	for i, col := range t.cols {
		colNames[i] = quoteIdent(col.name)
	}
	placeholder := "(" + strings.TrimSuffix(strings.Repeat("?,", len(t.cols)), ",") + ")"
	query := fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", t.name, strings.Join(colNames, ", "), placeholder)

	if _, err := runner.ExecContext(ctx, query, row...); err != nil {
		var myErr *mysqldriver.MySQLError
		if !errors.As(err, &myErr) || myErr.Number != 1062 {
			return 0, false, err
		}
		existing, lookupErr := lookupExistingID(ctx, runner, t, row, naturalIdx)
		if lookupErr != nil {
			return 0, false, fmt.Errorf("duplicate key (%v), then failed to look up existing row: %w", err, lookupErr)
		}
		return existing, true, nil
	}
	idIdx := colIndex(t, "id")
	id, _ := row[idIdx].(int64)
	return id, false, nil
}

func lookupExistingID(ctx context.Context, runner sqlRunner, t migrateTable, row []interface{}, naturalIdx []int) (int64, error) {
	where := make([]string, len(naturalIdx))
	args := make([]interface{}, 0, len(naturalIdx))
	for i, idx := range naturalIdx {
		if row[idx] == nil {
			where[i] = quoteIdent(t.cols[idx].name) + " IS NULL"
			continue
		}
		where[i] = quoteIdent(t.cols[idx].name) + " = ?"
		args = append(args, row[idx])
	}
	q := fmt.Sprintf("SELECT id FROM %s WHERE %s", t.name, strings.Join(where, " AND "))
	var id int64
	err := runner.QueryRowContext(ctx, q, args...).Scan(&id)
	return id, err
}

// readSourceRows runs the table's SELECT against the sqlite3 CLI in JSON
// mode and decodes it into ordered value slices matching t.cols.
func (imp *sqliteImporter) readSourceRows(t migrateTable) ([][]interface{}, error) {
	query := buildSelect(t)
	cmd := exec.Command(imp.sqlite3Path, "-readonly", "-json", imp.sourcePath, query) //nolint:gosec // fixed table/column set, no user input reaches the query
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("sqlite3 %s: %v: %s", t.name, err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) == "" {
		return nil, nil // empty table: sqlite3 -json prints nothing (not "[]")
	}

	dec := json.NewDecoder(&stdout)
	dec.UseNumber()
	var raw []map[string]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode sqlite3 json for %s: %w", t.name, err)
	}

	out := make([][]interface{}, 0, len(raw))
	for _, rowMap := range raw {
		vals := make([]interface{}, len(t.cols))
		for i, col := range t.cols {
			v, err := convertValue(rowMap[col.name], col.kind)
			if err != nil {
				return nil, fmt.Errorf("%s.%s: %w", t.name, col.name, err)
			}
			vals[i] = v
		}
		out = append(out, vals)
	}
	return out, nil
}

// buildSelect renders "SELECT col1, hex(col2) AS col2, ... FROM table" so
// BLOB columns survive the sqlite3 CLI's JSON text encoding (raw bytes would
// otherwise be corrupted — JSON has no binary type).
func buildSelect(t migrateTable) string {
	parts := make([]string, len(t.cols))
	for i, col := range t.cols {
		if col.kind == kindBlobHex {
			parts[i] = fmt.Sprintf("hex(%s) AS %s", col.name, col.name)
		} else {
			parts[i] = col.name
		}
	}
	return "SELECT " + strings.Join(parts, ", ") + " FROM " + t.name
}

func convertValue(v interface{}, kind migrateColKind) (interface{}, error) {
	if v == nil {
		return nil, nil
	}
	switch kind {
	case kindString:
		if s, ok := v.(string); ok {
			return s, nil
		}
		return fmt.Sprintf("%v", v), nil
	case kindInt:
		n, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected number, got %T", v)
		}
		i, err := n.Int64()
		if err != nil {
			// SQLite is dynamically typed; tolerate a float-formatted integer.
			f, ferr := n.Float64()
			if ferr != nil {
				return nil, err
			}
			return int64(f), nil
		}
		return i, nil
	case kindFloat:
		n, ok := v.(json.Number)
		if !ok {
			return nil, fmt.Errorf("expected number, got %T", v)
		}
		f, err := n.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	case kindBlobHex:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected hex string, got %T", v)
		}
		b, err := hex.DecodeString(s)
		if err != nil {
			return nil, fmt.Errorf("decode hex blob: %w", err)
		}
		return b, nil
	default:
		return nil, fmt.Errorf("unknown column kind %d", kind)
	}
}

// insertRowsIgnore batch-inserts rows for tables with no natural-key dedup
// risk. INSERT IGNORE tolerates the rare composite-key collision that can
// arise after applyRemap folds two source ids onto one (e.g. a join-table
// row that already existed for the surviving id); any resulting undercount
// is caught by the row-count verification in run().
func (imp *sqliteImporter) insertRowsIgnore(ctx context.Context, runner sqlRunner, t migrateTable, rows [][]interface{}) error {
	if len(rows) == 0 {
		return nil
	}
	colNames := make([]string, len(t.cols))
	for i, col := range t.cols {
		colNames[i] = quoteIdent(col.name)
	}
	colList := strings.Join(colNames, ", ")
	rowPlaceholder := "(" + strings.TrimSuffix(strings.Repeat("?,", len(t.cols)), ",") + ")"

	batch := imp.batchSize
	if batch <= 0 {
		batch = 500
	}
	for start := 0; start < len(rows); start += batch {
		end := start + batch
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		placeholders := make([]string, len(chunk))
		args := make([]interface{}, 0, len(chunk)*len(t.cols))
		for i, r := range chunk {
			placeholders[i] = rowPlaceholder
			args = append(args, r...)
		}
		query := fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES %s", t.name, colList, strings.Join(placeholders, ","))
		if _, err := runner.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("batch [%d:%d]: %w", start, end, err)
		}
	}
	return nil
}

// resetAutoIncrement sets table's AUTO_INCREMENT to MAX(id)+1 so future
// scanner/API inserts continue from the imported max id rather than
// colliding with (or restarting behind) preserved SQLite ids.
func resetAutoIncrement(ctx context.Context, conn *sql.Conn, table string) error {
	var next int64
	if err := conn.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) + 1 FROM "+table).Scan(&next); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s AUTO_INCREMENT = %d", table, next))
	return err
}
