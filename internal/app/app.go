package app

import (
	"context"
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
	server          *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
	db              *pgxpool.Pool
	photos          *media.Service
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
	handler := httpapi.New(cfg, logger, db, auth.New(db, cfg, mail.New(cfg)), users.New(db), listings.New(db), locations.New(db), favorites.New(db), stars.New(db), moderation.New(db), photos, logs)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: cfg.ReadHeaderTimeout, ReadTimeout: cfg.ReadTimeout, WriteTimeout: cfg.WriteTimeout, IdleTimeout: cfg.IdleTimeout, MaxHeaderBytes: 32 * 1024, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError)}
	ok = true
	return &App{server: server, logger: logger, shutdownTimeout: cfg.ShutdownTimeout, db: db, photos: photos}, nil
}
func (a *App) Run(ctx context.Context) error {
	defer a.db.Close()
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
				if err := a.photos.FlushDeletions(cleanupCtx); err != nil {
					a.logger.Error("file cleanup retry failed")
				}
			}
		}
	}()
	defer func() { stopCleanup(); <-cleanupDone }()

	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	serveErrors := make(chan error, 1)
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
	return serverError(<-serveErrors)
}
func serverError(err error) error {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("http server: %w", err)
}
