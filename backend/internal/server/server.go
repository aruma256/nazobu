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

	"golang.org/x/oauth2"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/config"
	"github.com/aruma256/nazobu/backend/internal/gen/nazobu/v1/nazobuv1connect"
	"github.com/aruma256/nazobu/backend/internal/oauth"
	"github.com/aruma256/nazobu/backend/internal/reminder"
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
	myPageService := newMyPageService(dbc)
	myPagePath, myPageHandler := nazobuv1connect.NewMyPageServiceHandler(myPageService)
	mux.Handle(myPagePath, myPageHandler)
	eventPath, eventHandler := nazobuv1connect.NewEventServiceHandler(newEventService(dbc))
	mux.Handle(eventPath, eventHandler)
	ticketPath, ticketHandler := nazobuv1connect.NewTicketServiceHandler(newTicketService(dbc))
	mux.Handle(ticketPath, ticketHandler)

	// Claude connector（remote MCP）向けの OAuth 2.1 認可サーバと MCP エンドポイント。
	// 公開 origin は frontend と同一（rewrites 経由）なので issuer には FRONTEND_URL を流用する。
	oauthSrv := oauth.NewServer(dbc, srv.httpClient, cfg.FrontendURL, cfg.CookieSecure)
	mux.HandleFunc("GET /.well-known/oauth-authorization-server", oauthSrv.HandleAuthorizationServerMetadata)
	mux.HandleFunc("GET /.well-known/oauth-protected-resource", oauthSrv.HandleProtectedResourceMetadata)
	// MCP パス付きの variant（RFC 9728 のパスサフィックス探索に対応）。
	mux.HandleFunc("GET /.well-known/oauth-protected-resource/mcp", oauthSrv.HandleProtectedResourceMetadata)
	mux.HandleFunc("GET /oauth/authorize", oauthSrv.HandleAuthorizeGet)
	mux.HandleFunc("POST /oauth/authorize", oauthSrv.HandleAuthorizePost)
	mux.HandleFunc("POST /oauth/token", oauthSrv.HandleToken)
	mux.Handle("/mcp", oauthSrv.Middleware(newMCPHandler(myPageService)))

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// リマインド通知ワーカー。webhook URL 未設定（ローカル開発の既定）なら起動しない。
	if cfg.Discord.WebhookURL != "" {
		go reminder.NewWorker(dbc, srv.httpClient, cfg.Discord.WebhookURL, cfg.FrontendURL).Run(ctx)
		fmt.Println("リマインド通知ワーカーを起動")
	} else {
		fmt.Println("DISCORD_WEBHOOK_URL 未設定のためリマインド通知ワーカーは起動しない")
	}

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
