// Command server runs the imapsync Web UI API.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/imapsync/web-backend/internal/api"
	"github.com/imapsync/web-backend/internal/config"
	"github.com/imapsync/web-backend/internal/runner"
	"github.com/imapsync/web-backend/internal/security"
	"github.com/imapsync/web-backend/internal/services"
	"github.com/imapsync/web-backend/internal/store"
	"github.com/imapsync/web-backend/internal/telegram"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	sec, err := security.New(cfg.SecretKey, cfg.FernetKey, cfg.AccessTokenTTL)
	if err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(cfg.DBPath())
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	// Prove the configured Fernet key is the one this database was written
	// with, before anything can encrypt under a different one. A key that
	// merely decodes was accepted, so restoring the wrong .env started cleanly:
	// every sync then failed at decrypt, and any profile edited afterwards was
	// re-encrypted under the new key -- leaving a database that no single key
	// can fully read, which no later fix recovers.
	if ct, err := db.AnyStoredCiphertext(); err != nil {
		log.Fatalf("checking the encryption key: %v", err)
	} else if ct != "" {
		if _, err := sec.Decrypt(ct); err != nil {
			log.Fatalf("IMAPSYNC_FERNET_KEY does not match the data in %s: "+
				"stored passwords cannot be decrypted with it. Restore the key this "+
				"database was created with; starting with the wrong one would "+
				"re-encrypt edited rows under it and make the mismatch permanent.",
				cfg.DBPath())
		}
	}

	if n, err := db.ReconcileOrphans(); err != nil {
		log.Printf("reconcile orphan jobs: %v", err)
	} else if n > 0 {
		log.Printf("marked %d job(s) left running by a previous shutdown as interrupted", n)
	}
	if n, err := db.ReconcileOrphanBatches(); err != nil {
		log.Printf("reconcile orphan batches: %v", err)
	} else if n > 0 {
		log.Printf("marked %d batch(es) left running by a previous shutdown as interrupted, "+
			"and cleared the credentials they still held", n)
	}

	run := runner.New(cfg.ImapsyncPath, cfg.TmpDir(), cfg.CacheDir(), cfg.LogDir)
	// Passfiles hold cleartext mailbox passwords and only exist for the length
	// of a run. Any still here belong to a run that did not end cleanly.
	if n, err := run.SweepTmp(); err != nil {
		log.Printf("sweeping the temp directory: %v", err)
	} else if n > 0 {
		log.Printf("removed %d leftover passfile(s)/pidfile(s) from an earlier run", n)
	}

	starter := &services.Starter{DB: db, Sec: sec, Runner: run}
	mailer := &services.Mailer{DB: db, Sec: sec}
	completion := &services.Completion{DB: db, Sec: sec, Mailer: mailer}
	run.OnComplete = completion.Finish

	scheduler := &services.Scheduler{DB: db, Starter: starter}
	scheduler.Start()
	defer scheduler.Stop()

	batch := &services.Batch{DB: db, Sec: sec, Starter: starter, Runner: run}
	hooks := services.NewWebhooks(db, sec)
	completion.Hooks = hooks

	retention := &services.Retention{DB: db, Runner: run}
	retention.Start()
	defer retention.Stop()

	// The bot reads its settings from the database, so it is a no-op until one
	// is saved on the Settings page.
	bot := telegram.New(db, sec, run, starter, batch)
	completion.Notify = bot.NotifyJobFinished
	// Two listeners, one signal: History needs the counters in the database, the
	// bot needs to decide whether a milestone has been crossed.
	run.OnProgress = func(jobID int64, p runner.Progress) {
		if err := db.UpdateJobCounts(jobID, p.Messages, p.Bytes); err != nil {
			log.Printf("job %d: recording progress: %v", jobID, err)
		}
		bot.OnProgress(jobID, p)
	}
	bot.Start()
	defer bot.Stop()

	srv := &api.Server{
		Cfg: cfg, DB: db, Sec: sec, Runner: run,
		Starter:   starter,
		Batch:     batch,
		Folders:   &services.Folders{ImapsyncPath: cfg.ImapsyncPath, TmpDir: cfg.TmpDir()},
		Mailer:    mailer,
		Cron:      scheduler,
		Bot:       bot,
		Hooks:     hooks,
		Retention: retention,
	}

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 15 * time.Second,
		// No write timeout: the job WebSocket streams for as long as a sync runs.
	}

	go func() {
		log.Printf("imapsync Web UI listening on %s", cfg.Listen)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Println("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
}
