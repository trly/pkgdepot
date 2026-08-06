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

	"github.com/trly/pkgdepot/internal/command"
	"github.com/trly/pkgdepot/internal/config"
	"github.com/trly/pkgdepot/internal/httpapi"
	"github.com/trly/pkgdepot/internal/repository"
	"github.com/trly/pkgdepot/internal/token"
	"github.com/urfave/cli/v3"
)

func serveCommand() *cli.Command {
	return &cli.Command{
		Name:   "serve",
		Usage:  "run the package repository server",
		Action: serve,
	}
}

func serve(ctx context.Context, _ *cli.Command) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	repositories := repository.New(cfg.DataRoot, command.NewPacman())
	if err := repositories.Initialize(); err != nil {
		return err
	}
	tokens := token.New(cfg.DataRoot)
	if err := tokens.Initialize(); err != nil {
		return err
	}
	server := &http.Server{
		Addr:         cfg.Address,
		Handler:      httpapi.New(repositories, tokens, cfg.URL, httpapi.Options{MaxUploadSize: cfg.MaxUploadSize}),
		ReadTimeout:  cfg.HTTPTimeout,
		WriteTimeout: cfg.HTTPTimeout,
		IdleTimeout:  cfg.HTTPTimeout,
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	log.Printf("listening on %s", cfg.Address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
