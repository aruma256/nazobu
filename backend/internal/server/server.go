package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/grpchealth"
	"golang.org/x/oauth2"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
)

type Server struct {
	cfg          config.Config
	db           *sql.DB
	discordOAuth *oauth2.Config
	httpClient   *http.Client
}

func Run(ctx context.Context, cfg config.Config, dbc *sql.DB) error {
	provider := auth.NewDiscordProvider(ctx)
	srv := &Server{
		cfg: cfg,
		db:  dbc,
		discordOAuth: auth.DiscordOAuthConfig(
			provider,
			cfg.Discord.ClientID,
			cfg.Discord.ClientSecret,
			cfg.Discord.RedirectURL,
		),
		httpClient: http.DefaultClient,
	}

	mux := http.NewServeMux()

	// gRPC health check (Connect)
	healthChecker := grpchealth.NewStaticChecker("nazobu")
	healthPath, healthHandler := grpchealth.NewHandler(healthChecker)
	mux.Handle(healthPath, healthHandler)

	// liveness 用
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})

	// 認証
	mux.HandleFunc("GET /auth/discord/login", srv.handleDiscordLogin)
	mux.HandleFunc("GET /auth/discord/callback", srv.handleDiscordCallback)
	mux.HandleFunc("POST /auth/logout", srv.handleLogout)

	// Connect RPC
	userPath, userHandler := nazobuv1connect.NewUserServiceHandler(newUserService(dbc))
	mux.Handle(userPath, userHandler)
	myPagePath, myPageHandler := nazobuv1connect.NewMyPageServiceHandler(newMyPageService(dbc))
	mux.Handle(myPagePath, myPageHandler)
	eventPath, eventHandler := nazobuv1connect.NewEventServiceHandler(newEventService(dbc))
	mux.Handle(eventPath, eventHandler)
	ticketPath, ticketHandler := nazobuv1connect.NewTicketServiceHandler(newTicketService(dbc))
	mux.Handle(ticketPath, ticketHandler)

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		fmt.Printf("listen %s\n", cfg.HTTPAddr)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}
