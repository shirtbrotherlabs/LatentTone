// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15
// Last-Modified: 2026-07-18

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/shirtbrotherlabs/LatentTone/internal/auth"
	"github.com/shirtbrotherlabs/LatentTone/internal/config"
	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/meta"
	"github.com/shirtbrotherlabs/LatentTone/internal/scan"
	"github.com/shirtbrotherlabs/LatentTone/internal/web"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("latenttone: ")

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	switch cmd {
	case "scan":
		os.Exit(runScan(os.Args[2:]))
	case "serve":
		os.Exit(runServe(os.Args[2:]))
	case "embed":
		os.Exit(runEmbed(os.Args[2:]))
	case "migrate-sqlite":
		os.Exit(migrateSqlite(os.Args[2:]))
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `LatentTone — library catalog, browse UI, and feature embedding

Usage:
  latenttone scan            [--config scanner.yaml] [--force]
  latenttone serve           [--config scanner.yaml] [--meta metadata.yaml] [--scan-on-start=true|false]
  latenttone embed           [--meta metadata.yaml] [--stop]
  latenttone migrate-sqlite  [--source /data/latenttone.db] [--config scanner.yaml] [--yes]
                             one-shot import of a legacy SQLite catalog into MariaDB
                             (dry run by default; requires the sqlite3 CLI on PATH)

Serve defaults: library scan on start (disable with --scan-on-start=false or
LATENTTONE_SCAN_ON_START=0). Periodic scans default to every 24h (admin-tunable
via Settings / GET|PATCH /api/scan/schedule; DB wins over yaml after first seed).

`)
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	cfgPath := fs.String("config", "/config/scanner.yaml", "path to scanner.yaml")
	force := fs.Bool("force", false, "re-extract all files even when mtime+size match")
	_ = fs.Parse(args)
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Println(err)
		return 1
	}
	if err := scan.LibraryReadable(cfg.LibraryRoot); err != nil {
		log.Printf("library root: %v", err)
		return 1
	}
	catalog, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer catalog.Close()

	sc := &scan.Scanner{Cfg: cfg, DB: catalog}
	res, err := sc.FullOpts("cli", scan.Options{Force: *force})
	if err != nil {
		log.Println(err)
		return 1
	}
	log.Printf("done: seen=%d upserted=%d skipped=%d missing=%d errors=%d",
		res.Seen, res.Upserted, res.Skipped, res.Missing, res.Errors)
	return 0
}

func runEmbed(args []string) int {
	fs := flag.NewFlagSet("embed", flag.ExitOnError)
	metaPath := fs.String("meta", "/config/metadata.yaml", "path to metadata.yaml")
	stopOnly := fs.Bool("stop", false, "signal a running embed to stop (writes job control file)")
	_ = fs.Parse(args)

	mcfg, err := meta.Load(*metaPath)
	if err != nil {
		log.Println(err)
		return 1
	}
	if *stopOnly {
		if err := meta.RequestStopFile(mcfg.JobControlPath); err != nil {
			log.Println(err)
			return 1
		}
		log.Printf("wrote stop request to %s", mcfg.JobControlPath)
		return 0
	}

	if err := scan.LibraryReadable(mcfg.LibraryRoot); err != nil {
		log.Printf("library root: %v", err)
		return 1
	}
	catalog, err := db.Open(mcfg.DatabaseDSN)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer catalog.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	res, err := meta.Run(ctx, mcfg, catalog, "cli", nil)
	if err != nil {
		log.Println(err)
		return 1
	}
	log.Printf("done: claimed=%d ok=%d errors=%d", res.Claimed, res.OK, res.Errors)
	return 0
}

func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	cfgPath := fs.String("config", "/config/scanner.yaml", "path to scanner.yaml")
	metaPath := fs.String("meta", "/config/metadata.yaml", "path to metadata.yaml")
	scanOnStart := fs.Bool("scan-on-start", true, "run a full library scan when the server starts (default on)")
	_ = fs.Parse(args)

	doScanOnStart := *scanOnStart
	if v, ok := envBool("LATENTTONE_SCAN_ON_START"); ok {
		doScanOnStart = v
	}

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Println(err)
		return 1
	}
	mcfg, err := meta.Load(*metaPath)
	if err != nil {
		log.Println(err)
		return 1
	}
	// Prefer scanner DB/library paths as share with browse; override meta DB from scanner if same compose volume.
	if cfg.DatabaseDSN != "" {
		mcfg.DatabaseDSN = cfg.DatabaseDSN
	}
	if cfg.LibraryRoot != "" {
		mcfg.LibraryRoot = cfg.LibraryRoot
	}

	if err := scan.LibraryReadable(cfg.LibraryRoot); err != nil {
		log.Printf("library root: %v", err)
		return 1
	}
	catalog, err := db.Open(cfg.DatabaseDSN)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer catalog.Close()

	// Seed scan_schedule once: yaml scan_interval=0 disables; otherwise bootstrap
	// interval (yaml if set, else 24h). Admin edits persist in DB and win thereafter.
	seedDisabled := cfg.ScanInterval == 0
	bootstrapSecs := db.DefaultScanIntervalSeconds
	if cfg.ScanInterval > 0 {
		bootstrapSecs = int(cfg.ScanInterval.Seconds())
	}
	if err := catalog.EnsureScanScheduleRow(bootstrapSecs, seedDisabled); err != nil {
		log.Printf("scan schedule seed: %v", err)
		return 1
	}

	sc := &scan.Scanner{Cfg: cfg, DB: catalog}
	embedCtrl := meta.NewController(mcfg.JobControlPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := web.New(cfg, mcfg, catalog, sc, embedCtrl, ctx)
	if err != nil {
		log.Println(err)
		return 1
	}

	if err := auth.BootstrapAdmin(catalog, cfg.AdminUsername, cfg.AdminPassword); err != nil {
		log.Printf("admin bootstrap: %v", err)
		return 1
	}
	if strings.TrimSpace(cfg.AdminUsername) != "" {
		log.Printf("admin account ready (username=%s)", cfg.AdminUsername)
	}

	// Library scan first; acoustic identity starts only after that finishes
	// (so new/changed tracks are in the catalog before embed claims work).
	if doScanOnStart {
		go func() {
			if _, err := sc.Full("startup"); err != nil {
				if !errors.Is(err, scan.ErrAlreadyRunning) {
					log.Printf("startup scan: %v", err)
				}
			}
			meta.StartIfIncomplete(ctx, embedCtrl, mcfg, catalog, "post-scan")
		}()
	} else {
		log.Printf("startup scan disabled (--scan-on-start=false or LATENTTONE_SCAN_ON_START=0)")
		go meta.StartIfIncomplete(ctx, embedCtrl, mcfg, catalog, "startup")
	}

	stopWatch := make(chan struct{})
	go func() {
		if err := sc.Watch(stopWatch); err != nil {
			log.Printf("watcher: %v", err)
		}
	}()

	go runPeriodicScanLoop(ctx, catalog, sc, embedCtrl, mcfg)

	if mcfg.EmbedInterval > 0 {
		go func() {
			t := time.NewTicker(mcfg.EmbedInterval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if embedCtrl.Running() {
						continue
					}
					if err := embedCtrl.Start(ctx, mcfg, catalog, "periodic"); err != nil {
						log.Printf("periodic embed: %v", err)
					}
				}
			}
		}()
	}

	httpSrv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		close(stopWatch)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s public_base_url=%s auth_mode=%s stream_probe=%v api_docs=%v (catalog browse + Phase 3 APIs)",
		cfg.ListenAddr, cfg.PublicBaseURL, cfg.AuthMode, cfg.EnableStreamProbe, cfg.EnableAPIDocs)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println(err)
		return 1
	}
	return 0
}

// runPeriodicScanLoop honors the persisted admin scan schedule (re-read each cycle).
func runPeriodicScanLoop(ctx context.Context, catalog *db.DB, sc *scan.Scanner, embedCtrl *meta.Controller, mcfg *meta.Config) {
	for {
		sched, err := catalog.GetScanSchedule()
		if err != nil {
			log.Printf("scan schedule: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
				continue
			}
		}
		if !sched.Enabled {
			web.SetNextPeriodicScanAt(time.Time{})
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
				continue
			}
		}
		wait := sched.Duration()
		next := time.Now().UTC().Add(wait)
		web.SetNextPeriodicScanAt(next)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if sc.Running() {
				log.Printf("periodic scan skipped: already running")
				continue
			}
			if _, err := sc.Full("periodic"); err != nil {
				if !errors.Is(err, scan.ErrAlreadyRunning) {
					log.Printf("periodic scan: %v", err)
				}
				continue
			}
			meta.StartIfIncomplete(ctx, embedCtrl, mcfg, catalog, "post-scan")
		}
	}
}

func envBool(key string) (bool, bool) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return false, false
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}
