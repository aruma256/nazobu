package oauth

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aruma256/nazobu/backend/internal/auth"
	"github.com/aruma256/nazobu/backend/internal/gen/queries"
	"github.com/aruma256/nazobu/backend/internal/id"
)

const (
	// 同意フォームの CSRF 対策用 cookie（double submit cookie 方式）。
	consentCSRFCookieName = "nazobu_oauth_consent_csrf"
	consentCSRFCookieTTL  = 10 * time.Minute
)

// authorizeParams は認可リクエストのうち、クライアント検証後にチェックするパラメータ群。
type authorizeParams struct {
	responseType  string
	scope         string
	state         string
	codeChallenge string
}

// authorizeError は redirect_uri へ error= 付きで戻すエラー（RFC 6749 4.1.2.1）。
type authorizeError struct {
	code        string
	description string
}

// parseAuthorizeParams は response_type / PKCE / scope / resource を検証する。
// redirect_uri の検証は済んでいる前提（エラーは redirect で返せる段階）。
func parseAuthorizeParams(v url.Values, resourceURL string) (authorizeParams, *authorizeError) {
	p := authorizeParams{
		responseType:  v.Get("response_type"),
		scope:         strings.TrimSpace(v.Get("scope")),
		state:         v.Get("state"),
		codeChallenge: v.Get("code_challenge"),
	}
	if p.responseType != "code" {
		return p, &authorizeError{"unsupported_response_type", "response_type は code のみ"}
	}
	// PKCE S256 を必須にする（OAuth 2.1 / MCP 仕様）。
	if p.codeChallenge == "" {
		return p, &authorizeError{"invalid_request", "code_challenge は必須"}
	}
	if len(p.codeChallenge) < 43 || len(p.codeChallenge) > 128 {
		return p, &authorizeError{"invalid_request", "code_challenge の長さが不正"}
	}
	if m := v.Get("code_challenge_method"); m != "S256" {
		return p, &authorizeError{"invalid_request", "code_challenge_method は S256 のみ"}
	}
	// scope: 未指定なら全 scope を既定にする（scope を送らないクライアントでも
	// 書き込みツールまで使えるように。付与内容は同意画面に明示する）。未知の scope は拒否する。
	if p.scope == "" {
		p.scope = ScopeRead + " " + ScopeWrite
	}
	for _, sc := range strings.Fields(p.scope) {
		if sc != ScopeRead && sc != ScopeWrite {
			return p, &authorizeError{"invalid_scope", fmt.Sprintf("未対応の scope: %s", sc)}
		}
	}
	// resource（RFC 8707）: 指定されている場合は本サーバの MCP エンドポイントと一致すること。
	if res := v.Get("resource"); res != "" {
		if strings.TrimSuffix(res, "/") != strings.TrimSuffix(resourceURL, "/") {
			return p, &authorizeError{"invalid_target", "resource が本サーバの MCP エンドポイントと一致しない"}
		}
	}
	return p, nil
}

// sessionUser は web と同じ session cookie からログインユーザーを引く。未ログインは nil。
func (s *Server) sessionUser(r *http.Request) *auth.User {
	c, err := r.Cookie(auth.SessionCookieName)
	if err != nil || c.Value == "" {
		return nil
	}
	user, err := auth.LookupSession(r.Context(), s.db, c.Value)
	if err != nil {
		return nil
	}
	return user
}

func (s *Server) HandleAuthorizeGet(w http.ResponseWriter, r *http.Request) {
	// 未ログインなら Discord ログインへ。完了後に同じ認可 URL へ戻す。
	user := s.sessionUser(r)
	if user == nil {
		http.Redirect(w, r, "/auth/discord/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusFound)
		return
	}

	q := r.URL.Query()
	meta, redirectURI, ok := s.resolveClientAndRedirect(w, r, q.Get("client_id"), q.Get("redirect_uri"))
	if !ok {
		return
	}
	params, aerr := parseAuthorizeParams(q, s.ResourceURL())
	if aerr != nil {
		redirectAuthorizeError(w, r, redirectURI, params.state, aerr)
		return
	}

	csrf, err := generateToken()
	if err != nil {
		s.renderErrorPage(w, http.StatusInternalServerError, "CSRF トークンの生成に失敗した")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     consentCSRFCookieName,
		Value:    csrf,
		Path:     "/oauth/authorize",
		MaxAge:   int(consentCSRFCookieTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})

	clientName := meta.ClientName
	if clientName == "" {
		clientName = meta.ClientID
	}
	s.renderTemplate(w, http.StatusOK, consentTemplate, consentView{
		UserName:         user.DisplayName,
		ClientName:       clientName,
		ClientID:         meta.ClientID,
		RedirectURI:      redirectURI,
		LoopbackRedirect: isLoopbackRedirectURI(redirectURI),
		Scope:            params.scope,
		ScopeWrite:       scopeContains(params.scope, ScopeWrite),
		State:            params.state,
		CodeChallenge:    params.codeChallenge,
		CSRFToken:        csrf,
	})
}

func (s *Server) HandleAuthorizePost(w http.ResponseWriter, r *http.Request) {
	user := s.sessionUser(r)
	if user == nil {
		s.renderErrorPage(w, http.StatusUnauthorized, "ログインセッションが切れている。最初からやり直してほしい")
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderErrorPage(w, http.StatusBadRequest, "フォームの解析に失敗した")
		return
	}

	// CSRF: cookie とフォームの値の一致を要求する。
	csrfCookie, err := r.Cookie(consentCSRFCookieName)
	if err != nil || csrfCookie.Value == "" || csrfCookie.Value != r.PostFormValue("csrf_token") {
		s.renderErrorPage(w, http.StatusBadRequest, "CSRF トークンが一致しない。最初からやり直してほしい")
		return
	}
	clearConsentCSRFCookie(w, s.cookieSecure)

	// hidden フィールドの値は改ざんされうるので、GET 時と同じ検証を最初からやり直す。
	form := r.PostForm
	_, redirectURI, ok := s.resolveClientAndRedirect(w, r, form.Get("client_id"), form.Get("redirect_uri"))
	if !ok {
		return
	}
	// フォームには response_type / code_challenge_method を載せないため補完して再検証する。
	form.Set("response_type", "code")
	form.Set("code_challenge_method", "S256")
	params, aerr := parseAuthorizeParams(form, s.ResourceURL())
	if aerr != nil {
		redirectAuthorizeError(w, r, redirectURI, params.state, aerr)
		return
	}

	if form.Get("action") != "approve" {
		redirectAuthorizeError(w, r, redirectURI, params.state, &authorizeError{"access_denied", "ユーザーが拒否した"})
		return
	}

	code, err := generateToken()
	if err != nil {
		s.renderErrorPage(w, http.StatusInternalServerError, "認可コードの生成に失敗した")
		return
	}
	if err := s.q.CreateOAuthAuthorizationCode(r.Context(), queries.CreateOAuthAuthorizationCodeParams{
		ID:            id.New(),
		CodeHash:      hashToken(code),
		UserID:        user.ID,
		ClientID:      form.Get("client_id"),
		RedirectUri:   redirectURI,
		Scope:         params.scope,
		CodeChallenge: params.codeChallenge,
		ExpiresAt:     s.now().Add(authorizationCodeTTL),
	}); err != nil {
		s.renderErrorPage(w, http.StatusInternalServerError, "認可コードの保存に失敗した")
		return
	}

	http.Redirect(w, r, buildRedirectURL(redirectURI, url.Values{
		"code":  {code},
		"state": {params.state},
	}), http.StatusFound)
}

// resolveClientAndRedirect は client_id（CIMD）と redirect_uri を検証する。
// この 2 つが不正な間は redirect でエラーを返してはいけない（open redirect になる）ため、
// 失敗時はエラーページを描画して ok=false を返す。
func (s *Server) resolveClientAndRedirect(w http.ResponseWriter, r *http.Request, clientID, redirectURI string) (*ClientMetadata, string, bool) {
	if clientID == "" {
		s.renderErrorPage(w, http.StatusBadRequest, "client_id は必須")
		return nil, "", false
	}
	meta, err := s.resolveClient(r.Context(), clientID)
	if err != nil {
		s.renderErrorPage(w, http.StatusBadRequest, "client_id の検証に失敗: "+err.Error())
		return nil, "", false
	}
	if redirectURI == "" {
		s.renderErrorPage(w, http.StatusBadRequest, "redirect_uri は必須")
		return nil, "", false
	}
	if !redirectURIAllowed(meta, redirectURI) {
		s.renderErrorPage(w, http.StatusBadRequest, "redirect_uri がクライアントに登録されていない")
		return nil, "", false
	}
	return meta, redirectURI, true
}

func redirectAuthorizeError(w http.ResponseWriter, r *http.Request, redirectURI, state string, aerr *authorizeError) {
	v := url.Values{"error": {aerr.code}}
	if aerr.description != "" {
		v.Set("error_description", aerr.description)
	}
	if state != "" {
		v.Set("state", state)
	}
	http.Redirect(w, r, buildRedirectURL(redirectURI, v), http.StatusFound)
}

// buildRedirectURL は redirect_uri の既存クエリを保ちつつパラメータを追記する。
func buildRedirectURL(redirectURI string, params url.Values) string {
	u, err := url.Parse(redirectURI)
	if err != nil {
		// redirect_uri は検証済みのためここには来ないが、保険としてそのまま返す。
		return redirectURI
	}
	q := u.Query()
	for k, vs := range params {
		for _, v := range vs {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func clearConsentCSRFCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     consentCSRFCookieName,
		Value:    "",
		Path:     "/oauth/authorize",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

type consentView struct {
	UserName         string
	ClientName       string
	ClientID         string
	RedirectURI      string
	LoopbackRedirect bool
	Scope            string
	// ScopeWrite は write scope を含むか（同意画面の「許可される操作」の出し分け用）。
	ScopeWrite bool
	State      string
	CodeChallenge    string
	CSRFToken        string
}

var consentTemplate = template.Must(template.New("consent").Parse(`<!DOCTYPE html>
<html lang="ja">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>アクセス許可の確認 - 謎部</title>
<style>
  body { font-family: sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem; line-height: 1.7; }
  .card { border: 1px solid #ccc; border-radius: 8px; padding: 1.5rem; }
  .client { font-weight: bold; }
  .meta { color: #666; font-size: 0.85rem; word-break: break-all; }
  .warn { background: #fff3cd; border: 1px solid #ffe08a; border-radius: 6px; padding: 0.5rem 0.75rem; font-size: 0.9rem; }
  .actions { display: flex; gap: 0.75rem; margin-top: 1.25rem; }
  button { padding: 0.6rem 1.5rem; border-radius: 6px; border: 1px solid #888; cursor: pointer; font-size: 1rem; }
  button.approve { background: #2563eb; border-color: #2563eb; color: #fff; }
</style>
</head>
<body>
<div class="card">
  <h1>アクセス許可の確認</h1>
  <p><span class="client">{{.ClientName}}</span> が、<strong>{{.UserName}}</strong> さんの謎部アカウントへのアクセスを求めています。</p>
  <p>許可される操作:</p>
  <ul>
    <li>自分の参加予定チケットやメンバー一覧などの読み取り</li>
    {{if .ScopeWrite}}<li>公演・チケットの新規登録などの書き込み</li>{{end}}
  </ul>
  <p class="meta">クライアント識別子: {{.ClientID}}<br>リダイレクト先: {{.RedirectURI}}</p>
  {{if .LoopbackRedirect}}<p class="warn">リダイレクト先がローカルアドレス（このパソコン上のアプリ）です。自分で開始した接続（Claude Code など）でない場合は拒否してください。</p>{{end}}
  <form method="post" action="/oauth/authorize">
    <input type="hidden" name="client_id" value="{{.ClientID}}">
    <input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
    <input type="hidden" name="scope" value="{{.Scope}}">
    <input type="hidden" name="state" value="{{.State}}">
    <input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
    <input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
    <div class="actions">
      <button class="approve" type="submit" name="action" value="approve">許可する</button>
      <button type="submit" name="action" value="deny">拒否する</button>
    </div>
  </form>
</div>
</body>
</html>
`))

type errorView struct {
	Message string
}

var errorTemplate = template.Must(template.New("error").Parse(`<!DOCTYPE html>
<html lang="ja">
<head><meta charset="utf-8"><title>エラー - 謎部</title></head>
<body style="font-family: sans-serif; max-width: 32rem; margin: 3rem auto; padding: 0 1rem;">
<h1>リクエストを処理できませんでした</h1>
<p>{{.Message}}</p>
</body>
</html>
`))

func (s *Server) renderTemplate(w http.ResponseWriter, status int, t *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = t.Execute(w, data)
}

func (s *Server) renderErrorPage(w http.ResponseWriter, status int, message string) {
	s.renderTemplate(w, status, errorTemplate, errorView{Message: message})
}
