package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/J0EY0/veyloom/internal/api"
	"github.com/J0EY0/veyloom/internal/config"
	"github.com/J0EY0/veyloom/internal/engine"
	"github.com/J0EY0/veyloom/internal/hub"
	"github.com/J0EY0/veyloom/internal/protocol"
	"github.com/J0EY0/veyloom/internal/worker"
)

// newServeCmd builds `veyloom serve`: a hub with one in-process worker for
// this machine, exposed over HTTP until the command's context is cancelled.
// Remote workers will connect to the same hub over the network later; the
// in-process one talks over a pipe but is otherwise identical.
func newServeCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the hub with a local worker",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServe(cmd, a.cfg)
		},
	}
	a.addDatabaseFlag(cmd)
	a.addStateDirFlag(cmd)
	a.addAddrFlag(cmd)
	a.addWorkerNameFlag(cmd)
	a.addDetectTimeoutFlag(cmd)
	return cmd
}

func runServe(cmd *cobra.Command, cfg config.Config) error {
	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	logger := slog.New(slog.NewTextHandler(cmd.ErrOrStderr(), nil))

	s, err := openStore(ctx, cfg.Database.URL)
	if err != nil {
		return err
	}
	defer s.Close()

	h := hub.New(s, cfg.Hub, hub.WithLogger(logger))
	discovery := worker.NewDiscovery(engine.Builtin(), cfg.Worker.DetectTimeout)
	identity := worker.FileIdentity{Path: cfg.WorkerIdentityPath()}
	local := worker.New(cfg.Worker, discovery, identity, engine.BuiltinRunners())

	// Any failure on either side of the pipe stops the whole process; on
	// one machine there is nothing to fall back to.
	hubEnd, workerEnd := protocol.Pipe()
	go func() {
		if err := h.Serve(ctx, hubEnd); err != nil {
			logger.Error("hub connection failed", "err", err)
			cancel()
		}
	}()
	go func() {
		if err := local.Run(ctx, workerEnd); err != nil {
			logger.Error("local worker failed", "err", err)
			cancel()
		}
	}()

	srv := &http.Server{
		Addr: cfg.Server.Addr,
		Handler: api.NewHandler(api.Deps{
			Engines:   discovery,
			Workers:   h,
			Projects:  s,
			Users:     s,
			Messages:  s,
			Agents:    s,
			Turns:     s,
			Approvals: s,
			Chat:      h,
			Events:    api.EventsOptions{AllowedOrigins: cfg.Server.AllowedOrigins, WriteTimeout: cfg.Server.WriteTimeout},
			Logger:    logger,
		}),
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
	}
	logger.Info("listening", "url", "http://"+cfg.Server.Addr)
	return serveUntilCancelled(ctx, srv, cfg.Server)
}

// serveUntilCancelled runs srv and shuts it down gracefully when ctx is
// cancelled, giving in-flight requests up to the configured timeout.
func serveUntilCancelled(ctx context.Context, srv *http.Server, cfg config.Server) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
