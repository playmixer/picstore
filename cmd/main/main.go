package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"picstore/internal/adapters/api/rest"
	"picstore/internal/adapters/config"
	"picstore/internal/adapters/storage"
	"picstore/internal/core/auth"
	"picstore/internal/core/picstore"
	"syscall"
	"time"

	"github.com/playmixer/single-auth/pkg/logger"
	"go.uber.org/zap"
)

var (
	shutdownDelay = time.Second * 2
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)
	defer stop()

	cfg, err := config.Init()
	if err != nil {
		return fmt.Errorf("failed initialize config: %w", err)
	}

	lgr, err := logger.New(ctx, logger.SetLevel(cfg.LogLevel), logger.SetLogPath(cfg.LogDir))
	if err != nil {
		return fmt.Errorf("failed initialize logger: %w", err)
	}

	store, err := storage.New(cfg.Store)
	if err != nil {
		lgr.Error("failed initialize storage", zap.Error(err))
		return fmt.Errorf("failed initialize storage: %w", err)
	}

	cache, err := storage.NewCache(cfg.Cache)
	if err != nil {
		lgr.Error("failed initialize cache", zap.Error(err))
		return fmt.Errorf("failed initialize cache: %w", err)
	}

	pic, err := picstore.New(ctx, cfg.Pic, lgr, store, cache)
	if err != nil {
		return fmt.Errorf("failed initialize core picstore: %w", err)
	}

	authManager, err := auth.New(store)
	if err != nil {
		return fmt.Errorf("faield initiale auth manager: %w", err)
	}

	httpServer := rest.New(
		pic,
		authManager,
		cache,
		lgr,
		rest.Addr(cfg.API.Addr),
		rest.BaseURL(cfg.API.BaseURL),
		rest.SetCookieDomain(cfg.API.CookieDomain.List()),
		rest.SetCookieLifeTime(cfg.API.CookieLifeTime),
		rest.SetSSOAuth(cfg.API.SSOAuthURL, cfg.API.SSOAuthCert),
	)

	lgr.Info("Starting")
	go func() {
		err = httpServer.Run()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			lgr.Error("stop http server", zap.Error(err))
		}
	}()
	<-ctx.Done()
	lgr.Info("Stopping...")
	ctxShutdown, cancel := context.WithTimeout(context.Background(), shutdownDelay)
	defer cancel()

	httpServer.Stop() // отключаем http сервер.
	// short.Wait()      // ждем завершения горитин.
	store.Close() // закрываем соединение с бд.

	<-ctxShutdown.Done()
	lgr.Info("Service stoped")
	lgr.Sync()
	return nil
}
