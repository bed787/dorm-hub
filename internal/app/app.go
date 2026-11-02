package app

import (
	"context"
	"dorm-hub/internal/analytics"
	"dorm-hub/internal/auth"
	"dorm-hub/internal/config"
	"dorm-hub/internal/favorites"
	"dorm-hub/internal/httpapi"
	"dorm-hub/internal/listings"
	"dorm-hub/internal/locations"
	"dorm-hub/internal/media"
	"dorm-hub/internal/moderation"
	"dorm-hub/internal/platform/filestore"
	"dorm-hub/internal/platform/logview"
	"dorm-hub/internal/platform/mail"
	"dorm-hub/internal/platform/postgres"
	"dorm-hub/internal/platform/search"
	"dorm-hub/internal/platform/telemetry"
	"dorm-hub/internal/stars"
	"dorm-hub/internal/users"
	"dorm-hub/migrations"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"log/slog"
	"net"
	"net/http"
	"time"
)

type App struct {
	metricsServer   *http.Server
	analytics       *analytics.Service
	searchSync      *listings.SearchSync
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	db              *pgxpool.Pool
	photos          *media.Service
	login           *auth.Service
}

func New(ctx context.Context, cfg config.Config, logger *slog.Logger) (*App, error) {
	logs := logview.New()
	logger = slog.New(logview.NewHandler(logger.Handler(), logs))
	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			db.Close()
		}
	}()
	migrationCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err = migrations.Apply(migrationCtx, db); err != nil {
		return nil, err
	}
	files, err := filestore.New(cfg.UploadDir)
	if err != nil {
		return nil, err
	}
	photos := media.NewWithLimits(db, files, cfg.UploadQuotaBytes, cfg.UploadPhotoLimit)
	if err = photos.BackfillSizes(ctx, logger); err != nil {
		return nil, fmt.Errorf("measure legacy photos: %w", err)
	}
	if err = photos.FlushDeletions(ctx); err != nil {
		return nil, fmt.Errorf("clean deleted files: %w", err)
	}
	metrics := telemetry.New()
	metrics.Database(db)
	login := auth.New(db, cfg, metrics.Mail(mail.New(cfg)))
	ads := listings.New(db)
	var searchSync *listings.SearchSync
	if cfg.MeiliURL != "" {
		engine, e := search.New(cfg.MeiliURL, cfg.MeiliMasterKey)
		if e != nil {
			return nil, e
		}
		ads = listings.NewWithSearch(db, engine)
		searchSync = listings.NewSearchSync(db, engine, logger)
	}
	handler := httpapi.NewWithMetrics(cfg, logger, db, login, users.New(db), ads, locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), photos, metrics, logs)
	var metricsServer *http.Server
	if cfg.MetricsAddr != "" {
		mux := http.NewServeMux()
		scrape := metrics.Handler()
		mux.Handle("GET /metrics", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), time.Second)
			defer cancel()
			var maintenance bool
			if db.QueryRow(ctx, `SELECT maintenance FROM site_settings WHERE singleton`).Scan(&maintenance) == nil {
				if maintenance {
					metrics.Maintenance.Set(1)
				} else {
					metrics.Maintenance.Set(0)
				}
				metrics.DatabaseUp.Set(1)
			} else {
				metrics.DatabaseUp.Set(0)
			}
			scrape.ServeHTTP(w, r)
		}))
		metricsServer = &http.Server{Addr: cfg.MetricsAddr, Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	}
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 32 * 1024, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError)}
	ok = true
	return &App{metricsServer: metricsServer, analytics: analytics.New(db), searchSync: searchSync, server: server, logger: logger, shutdownTimeout: cfg.ShutdownTimeout, db: db, photos: photos, login: login}, nil
}
func (a *App) Run(ctx context.Context) error {
	defer a.db.Close()
	if a.searchSync != nil {
		syncCtx, stopSync := context.WithCancel(ctx)
		syncDone := make(chan struct{})
		go func() { defer close(syncDone); a.searchSync.Run(syncCtx) }()
		defer func() { stopSync(); <-syncDone }()
	}
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() {
		defer close(cleanupDone)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-cleanupCtx.Done():
				return
			case <-ticker.C:
				passCtx, cancel := context.WithTimeout(cleanupCtx, 5*time.Second)
				if err := a.login.PruneExpired(passCtx); err != nil {
					a.logger.Error("expired authentication cleanup failed")
				}
				if err := a.photos.FlushDeletions(passCtx); err != nil {
					a.logger.Error("file cleanup retry failed")
				}
				if err := a.analytics.Prune(passCtx); err != nil {
					a.logger.Warn("analytics cleanup failed")
				}
				cancel()
			}
		}
	}()
	defer func() { stopCleanup(); <-cleanupDone }()

	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	serveErrors := make(chan error, 2)
	if a.metricsServer != nil {
		metricsListener, err := net.Listen("tcp", a.metricsServer.Addr)
		if err != nil {
			_ = listener.Close()
			return fmt.Errorf("metrics listen: %w", err)
		}
		defer a.metricsServer.Close()
		go func() { serveErrors <- a.metricsServer.Serve(metricsListener) }()
	}
	go func() { serveErrors <- a.server.Serve(listener) }()
	a.logger.Info("Dorm Hub started", "address", listener.Addr().String())
	select {
	case err := <-serveErrors:
		_ = a.server.Close()
		return serverError(err)
	case <-ctx.Done():
		a.logger.Info("Dorm Hub stopping")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()
	if err = a.server.Shutdown(shutdownCtx); err != nil {
		_ = a.server.Close()
		<-serveErrors
		return fmt.Errorf("shutdown: %w", err)
	}
	if a.metricsServer != nil {
		_ = a.metricsServer.Shutdown(shutdownCtx)
	}
	return serverError(<-serveErrors)
}
func serverError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("http server: %w", err)
}
