package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/trly/pkgdepot/internal/auth"
	"github.com/trly/pkgdepot/internal/command"
	"github.com/trly/pkgdepot/internal/config"
	"github.com/trly/pkgdepot/internal/httpapi"
	"github.com/trly/pkgdepot/internal/repository"
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
	resourceAuth, err := resourceServer(cfg)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:         cfg.Address,
		Handler:      httpapi.New(repositories, cfg.URL, httpapi.Options{AppName: cfg.AppName, MaxUploadSize: cfg.MaxUploadSize, ResourceAuth: resourceAuth}),
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

func resourceServer(cfg config.Config) (*auth.ResourceServer, error) {
	resourceURL := strings.TrimRight(cfg.URL, "/")
	audience := cfg.Auth.Audience
	if audience == "" {
		audience = resourceURL
	}
	discoveryCtx := oidc.ClientContext(context.Background(), &http.Client{Timeout: cfg.HTTPTimeout})
	validator, err := auth.NewOIDCValidator(discoveryCtx, auth.OIDCOptions{Issuer: cfg.Auth.Issuer, Audience: audience, Algorithms: cfg.Auth.Algorithms, KeyCacheLifetime: cfg.Auth.KeyCacheLifetime, RoleClaim: cfg.Auth.RoleClaim})
	if err != nil {
		return nil, err
	}
	return resourceServerWithValidator(cfg, validator), nil
}

func resourceServerWithValidator(cfg config.Config, validator auth.Validator) *auth.ResourceServer {
	return &auth.ResourceServer{
		Validator: validator,
		Metadata: auth.ResourceMetadata{
			Resource:               auth.NormalizeResourceIdentifier(cfg.URL),
			AuthorizationServers:   []string{cfg.Auth.Issuer},
			ScopesSupported:        []string{auth.ScopePublish, auth.ScopeRemove},
			BearerMethodsSupported: []string{"header"},
		},
		Authorize: func(claims auth.Claims, scope, _, _ string) bool {
			return auth.AuthorizeRoles(claims, scope, cfg.Auth.RoleScopes)
		},
	}
}
