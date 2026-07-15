// Copyright (C) 2026 martinsah
// SPDX-License-Identifier: GPL-3.0-only
// Author: martinsah
// Date: 2026-07-15

package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"

	"github.com/shirtbrotherlabs/LatentTone/internal/db"
	"github.com/shirtbrotherlabs/LatentTone/internal/extract"
	"github.com/shirtbrotherlabs/LatentTone/internal/lance"
)

// Result summarizes an embed run.
type Result struct {
	Claimed int
	OK      int
	Errors  int
}

// ExtractorProgress is live progress for one acoustic scanner in the current run.
type ExtractorProgress struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Done    int    `json:"done"`
	OK      int    `json:"ok"`
	Errors  int    `json:"errors"`
}

// Progress is live / last-run embed progress for the UI.
type Progress struct {
	Running    bool                `json:"running"`
	Claimed    int                 `json:"claimed"`
	Done       int                 `json:"done"`
	OK         int                 `json:"ok"`
	Errors     int                 `json:"errors"`
	Last       string              `json:"last"`
	Extractors []ExtractorProgress `json:"extractors"`
}

// Controller manages a single cancellable embed job (serve mode).
type Controller struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	running      bool
	last         string
	controlPath  string
	claimed      int
	done         int
	ok           int
	errs         int
	extractorOK  map[string]int
	extractorErr map[string]int
	enabledEx    []string
}

// NewController creates a job controller.
func NewController(jobControlPath string) *Controller {
	return &Controller{controlPath: jobControlPath}
}

// Running reports whether an embed is active.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// LastStatus returns last completed status line.
func (c *Controller) LastStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

// ProgressSnapshot returns live counters for the UI.
func (c *Controller) ProgressSnapshot() Progress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Progress{
		Running:    c.running,
		Claimed:    c.claimed,
		Done:       c.done,
		OK:         c.ok,
		Errors:     c.errs,
		Last:       c.last,
		Extractors: c.extractorProgressLocked(),
	}
}

func (c *Controller) extractorProgressLocked() []ExtractorProgress {
	enabled := make(map[string]bool, len(c.enabledEx))
	for _, name := range c.enabledEx {
		enabled[name] = true
	}
	out := make([]ExtractorProgress, 0, len(db.AcousticExtractors))
	for _, name := range db.AcousticExtractors {
		okN := 0
		errN := 0
		if c.extractorOK != nil {
			okN = c.extractorOK[name]
		}
		if c.extractorErr != nil {
			errN = c.extractorErr[name]
		}
		out = append(out, ExtractorProgress{
			Name:    name,
			Enabled: enabled[name],
			Done:    okN + errN,
			OK:      okN,
			Errors:  errN,
		})
	}
	return out
}

func (c *Controller) setEnabledExtractors(names []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabledEx = append([]string(nil), names...)
	c.extractorOK = make(map[string]int)
	c.extractorErr = make(map[string]int)
}

func (c *Controller) setClaimed(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.claimed = n
	c.done = 0
	c.ok = 0
	c.errs = 0
	c.extractorOK = make(map[string]int)
	c.extractorErr = make(map[string]int)
}

func (c *Controller) addDone(ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.done++
	if ok {
		c.ok++
	} else {
		c.errs++
	}
}

func (c *Controller) addExtractorDone(name string, ok bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.extractorOK == nil {
		c.extractorOK = make(map[string]int)
	}
	if c.extractorErr == nil {
		c.extractorErr = make(map[string]int)
	}
	if ok {
		c.extractorOK[name]++
	} else {
		c.extractorErr[name]++
	}
}

// Start launches Run in a goroutine if idle.
func (c *Controller) Start(parent context.Context, cfg *Config, catalog *db.DB, trigger string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		return fmt.Errorf("embed already running")
	}
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.running = true
	c.claimed = 0
	c.done = 0
	c.ok = 0
	c.errs = 0
	c.enabledEx = append([]string(nil), cfg.Extractors...)
	c.extractorOK = make(map[string]int)
	c.extractorErr = make(map[string]int)
	c.controlPath = cfg.JobControlPath
	go func() {
		res, err := Run(ctx, cfg, catalog, trigger, c)
		c.mu.Lock()
		c.running = false
		c.cancel = nil
		if err != nil {
			c.last = fmt.Sprintf("error: %v", err)
		} else if res != nil {
			c.claimed = res.Claimed
			c.ok = res.OK
			c.errs = res.Errors
			c.done = res.OK + res.Errors
			c.last = fmt.Sprintf("claimed=%d ok=%d err=%d", res.Claimed, res.OK, res.Errors)
		}
		c.mu.Unlock()
		clearJobControl(cfg.JobControlPath)
	}()
	_ = writeJobControl(cfg.JobControlPath, "running")
	return nil
}

// Stop cancels the active job.
func (c *Controller) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = writeJobControl(c.controlPath, "stop")
	if !c.running || c.cancel == nil {
		return fmt.Errorf("no embed running")
	}
	c.cancel()
	return nil
}

func writeJobControl(path, state string) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(state+"\n"), 0o644)
}

func clearJobControl(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// RequestStopFile writes a stop request for CLI/Compose coordination.
func RequestStopFile(path string) error {
	return writeJobControl(path, "stop")
}

func stopRequested(path string) bool {
	if path == "" {
		return false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return len(b) >= 4 && string(b[:4]) == "stop"
}

// Run executes one embed batch (sampleable, cancellable).
// progress may be nil (CLI one-shot); when set, live counters update for the UI.
func Run(ctx context.Context, cfg *Config, catalog *db.DB, trigger string, progress *Controller) (*Result, error) {
	lg := log.Default()
	_, _ = catalog.ResetStuckProcessing()

	extractorSet := cfg.ExtractorSetString()
	modelJSON, _ := json.Marshal(cfg.ModelVersions)
	modelVersions := string(modelJSON)

	if _, err := catalog.EnsureVectorRows(extractorSet, modelVersions); err != nil {
		return nil, err
	}
	if _, err := catalog.MarkStaleByConfig(extractorSet, modelVersions); err != nil {
		return nil, err
	}

	reg := extract.Registry(cfg.ModelVersions)
	if ex, ok := reg["essentia"].(*extract.Essentia); ok {
		if cfg.EssentiaBinary != "" {
			ex.Binary = cfg.EssentiaBinary
		}
		if cfg.EssentiaProfile != "" {
			ex.Profile = cfg.EssentiaProfile
		}
	}
	helper := extract.MLHelperConfig{HelperPath: cfg.MLHelperPath}
	if ex, ok := reg["yamnet"].(*extract.YAMNet); ok {
		ex.Helper = helper
		if cfg.YAMNetModel != "" {
			ex.Model = cfg.YAMNetModel
		}
		if cfg.YAMNetClassMap != "" {
			ex.ClassMap = cfg.YAMNetClassMap
		}
	}
	if ex, ok := reg["musicnn"].(*extract.MusiCNN); ok {
		ex.Helper = helper
		if cfg.MusiCNNModel != "" {
			ex.Model = cfg.MusiCNNModel
		}
		if cfg.MusiCNNMeta != "" {
			ex.Meta = cfg.MusiCNNMeta
		}
	}
	var active []extract.Extractor
	for _, name := range cfg.Extractors {
		ex, ok := reg[name]
		if !ok {
			return nil, fmt.Errorf("unknown extractor %q", name)
		}
		active = append(active, ex)
	}
	store := &lance.Store{
		DBPath:     cfg.LanceDBPath,
		Table:      cfg.LanceDBTable,
		HelperPath: cfg.LanceHelperPath,
	}

	runID, err := catalog.BeginEmbedRun(trigger, cfg.SampleMode, cfg.MaxTracks)
	if err != nil {
		return nil, err
	}
	_ = writeJobControl(cfg.JobControlPath, "running")

	limit := cfg.MaxTracks
	if cfg.SampleMode == "all" {
		limit = 1_000_000
	}

	ids, err := catalog.ClaimVectorWork(limit, cfg.SampleMode, cfg.SampleSeed)
	if err != nil {
		_ = catalog.FinishEmbedRun(runID, 0, 0, 0, "error", err.Error())
		return nil, err
	}
	if progress != nil {
		progress.setEnabledExtractors(cfg.Extractors)
		progress.setClaimed(len(ids))
	}

	var (
		okCount  atomic.Int64
		errCount atomic.Int64
		wg       sync.WaitGroup
		jobs     = make(chan int64, len(ids))
		lanceMu  sync.Mutex // serialize LanceDB upserts; acoustic extractors stay parallel across tracks
	)
	workers := cfg.Concurrency
	if workers < 1 {
		workers = 1
	}
	lg.Printf("embed workers=%d sample_mode=%s claimed=%d", workers, cfg.SampleMode, len(ids))
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for trackID := range jobs {
				if ctx.Err() != nil || stopRequested(cfg.JobControlPath) {
					_ = catalog.ReleaseProcessingToPending(trackID)
					continue
				}
				if err := processTrack(ctx, cfg, catalog, store, &lanceMu, progress, active, extractorSet, modelVersions, trackID); err != nil {
					lg.Printf("embed track %d: %v", trackID, err)
					errCount.Add(1)
					_ = catalog.MarkVectorError(trackID, err.Error())
					if progress != nil {
						progress.addDone(false)
					}
					continue
				}
				okCount.Add(1)
				if progress != nil {
					progress.addDone(true)
				}
			}
		}()
	}

	for _, id := range ids {
		if ctx.Err() != nil || stopRequested(cfg.JobControlPath) {
			_ = catalog.ReleaseProcessingToPending(id)
			continue
		}
		jobs <- id
	}
	close(jobs)
	wg.Wait()

	status := "ok"
	if ctx.Err() != nil || stopRequested(cfg.JobControlPath) {
		status = "stopped"
		_, _ = catalog.ResetStuckProcessing()
	}
	res := &Result{Claimed: len(ids), OK: int(okCount.Load()), Errors: int(errCount.Load())}
	_ = catalog.FinishEmbedRun(runID, res.Claimed, res.OK, res.Errors, status, "")
	clearJobControl(cfg.JobControlPath)
	lg.Printf("embed %s: claimed=%d ok=%d errors=%d", status, res.Claimed, res.OK, res.Errors)
	return res, nil
}

func processTrack(ctx context.Context, cfg *Config, catalog *db.DB, store *lance.Store, lanceMu *sync.Mutex, progress *Controller, active []extract.Extractor, extractorSet, modelVersions string, trackID int64) error {
	brief, err := catalog.GetTrackEmbedBrief(trackID)
	if err != nil {
		return err
	}
	if brief == nil {
		return fmt.Errorf("track %d not found", trackID)
	}
	var compiled []float32
	for _, ex := range active {
		if err := ctx.Err(); err != nil {
			_ = catalog.ReleaseProcessingToPending(trackID)
			return err
		}
		res, err := ex.Extract(ctx, cfg.LibraryRoot, brief)
		if err != nil {
			if progress != nil {
				progress.addExtractorDone(ex.Name(), false)
			}
			return err
		}
		js, err := extract.FeaturesJSON(res.Features)
		if err != nil {
			if progress != nil {
				progress.addExtractorDone(ex.Name(), false)
			}
			return err
		}
		if err := catalog.SaveTrackFeatures(trackID, res.Name, res.ModelVersion, js, len(res.Vector)); err != nil {
			if progress != nil {
				progress.addExtractorDone(ex.Name(), false)
			}
			return err
		}
		if progress != nil {
			progress.addExtractorDone(ex.Name(), true)
		}
		compiled = append(compiled, res.Vector...)
	}
	extract.L2Normalize(compiled)
	lanceID := ""
	if store != nil && store.Enabled() {
		if lanceMu != nil {
			lanceMu.Lock()
		}
		id, err := store.Upsert(ctx, trackID, compiled)
		if lanceMu != nil {
			lanceMu.Unlock()
		}
		if err != nil {
			return err
		}
		lanceID = id
	}
	return catalog.MarkVectorReady(trackID, extractorSet, modelVersions, compiled, brief.FileMtime, lanceID)
}
