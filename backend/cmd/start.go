package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/grpchealth"
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "HTTP サーバを起動する",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := config.Load()

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		mux := http.NewServeMux()

		// gRPC health check (Connect)
		healthChecker := grpchealth.NewStaticChecker("nazobu")
		healthPath, healthHandler := grpchealth.NewHandler(healthChecker)
		mux.Handle(healthPath, healthHandler)

		// 単純な liveness 用
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprintln(w, "ok")
		})

		// /api/me スタブ。session 機構が入るまでは常に 401 を返す。
		mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
		})

		srv := &http.Server{
			Addr:              cfg.HTTPAddr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		}

		errCh := make(chan error, 1)
		go func() {
			fmt.Printf("listen %s\n", cfg.HTTPAddr)
			errCh <- srv.ListenAndServe()
		}()

		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return srv.Shutdown(shutdownCtx)
		case err := <-errCh:
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return err
		}
	},
}
