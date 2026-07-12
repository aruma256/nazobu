package oauth

import (
	"encoding/json"
	"net/http"
)

// authorizationServerMetadata は RFC 8414 の Authorization Server Metadata。
// client_id_metadata_document_supported: true と token_endpoint_auth_methods_supported: ["none"]
// の両方を出すことで、Claude は DCR ではなく CIMD 方式を選択する。
type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	ScopesSupported                   []string `json:"scopes_supported"`
	ClientIDMetadataDocumentSupported bool     `json:"client_id_metadata_document_supported"`
}

// protectedResourceMetadata は RFC 9728 の Protected Resource Metadata。
// resource はユーザーが Claude に入力する MCP サーバ URL と完全一致させる必要がある。
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

func (s *Server) HandleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authorizationServerMetadata{
		Issuer:                            s.baseURL,
		AuthorizationEndpoint:             s.baseURL + "/oauth/authorize",
		TokenEndpoint:                     s.baseURL + "/oauth/token",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
		ScopesSupported:                   []string{ScopeRead, ScopeWrite},
		ClientIDMetadataDocumentSupported: true,
	})
}

func (s *Server) HandleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource:               s.ResourceURL(),
		AuthorizationServers:   []string{s.baseURL},
		ScopesSupported:        []string{ScopeRead, ScopeWrite},
		BearerMethodsSupported: []string{"header"},
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
