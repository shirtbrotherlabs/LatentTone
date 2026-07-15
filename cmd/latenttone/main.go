// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
  latenttone scan   [--config scanner.yaml]
  latenttone serve  [--config scanner.yaml] [--meta metadata.yaml] [--scan-on-start]
  latenttone embed  [--meta metadata.yaml] [--stop]

`)
}

func runScan(args []string) int {
	fs := flag.NewFlagSet("scan", flag.ExitOnError)
	cfgPath := fs.String("config", "/config/scanner.yaml", "path to scanner.yaml")
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
	catalog, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer catalog.Close()

	sc := &scan.Scanner{Cfg: cfg, DB: catalog}
	res, err := sc.Full("cli")
	if err != nil {
		log.Println(err)
		return 1
	}
	log.Printf("done: seen=%d upserted=%d missing=%d errors=%d", res.Seen, res.Upserted, res.Missing, res.Errors)
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
	catalog, err := db.Open(mcfg.DatabasePath)
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
	scanOnStart := fs.Bool("scan-on-start", false, "run a full library scan when the server starts")
	_ = fs.Parse(args)

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
	if cfg.DatabasePath != "" {
		mcfg.DatabasePath = cfg.DatabasePath
	}
	if cfg.LibraryRoot != "" {
		mcfg.LibraryRoot = cfg.LibraryRoot
	}

	if err := scan.LibraryReadable(cfg.LibraryRoot); err != nil {
		log.Printf("library root: %v", err)
		return 1
	}
	catalog, err := db.Open(cfg.DatabasePath)
	if err != nil {
		log.Println(err)
		return 1
	}
	defer catalog.Close()

	sc := &scan.Scanner{Cfg: cfg, DB: catalog}
	embedCtrl := meta.NewController(mcfg.JobControlPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv, err := web.New(cfg, mcfg, catalog, sc, embedCtrl, ctx)
	if err != nil {
		log.Println(err)
		return 1
	}

	if *scanOnStart {
		go func() {
			if _, err := sc.Full("startup"); err != nil {
				log.Printf("startup scan: %v", err)
			}
		}()
	}

	stopWatch := make(chan struct{})
	go func() {
		if err := sc.Watch(stopWatch); err != nil {
			log.Printf("watcher: %v", err)
		}
	}()

	if cfg.ScanInterval > 0 {
		go func() {
			t := time.NewTicker(cfg.ScanInterval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if _, err := sc.Full("periodic"); err != nil {
						log.Printf("periodic scan: %v", err)
					}
				}
			}
		}()
	}
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

	log.Printf("listening on %s auth_mode=%s stream_probe=%v api_docs=%v (catalog browse + Phase 3 APIs)",
		cfg.ListenAddr, cfg.AuthMode, cfg.EnableStreamProbe, cfg.EnableAPIDocs)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Println(err)
		return 1
	}
	return 0
}
