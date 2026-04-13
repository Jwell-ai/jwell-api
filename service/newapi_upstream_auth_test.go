package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/stretchr/testify/require"
)

func TestResolveNewAPIUpstreamAuthTokenCreatesAndCachesToken(t *testing.T) {
	t.Parallel()

	loginCount := 0
	tokenCreated := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/user/login":
			loginCount++
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id": 42,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			items := []map[string]any{}
			if tokenCreated {
				items = append(items, map[string]any{"id": 7, "name": "jwell-upstream"})
			}
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": items,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/":
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			tokenCreated = true
			writeNewAPITestJSON(t, w, map[string]any{"success": true, "message": "", "data": map[string]any{}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/7/key":
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": "sk-upstream",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), server.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-upstream", token)

	token, resolved, err = ResolveNewAPIUpstreamAuthToken(context.Background(), server.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-upstream", token)
	require.Equal(t, 1, loginCount)
}

func TestResolveNewAPIUpstreamAuthTokenIgnoresPlainKey(t *testing.T) {
	t.Parallel()

	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), "https://example.com", "sk-plain", "")
	require.NoError(t, err)
	require.False(t, resolved)
	require.Equal(t, "sk-plain", token)
}

func TestNewAPIUpstreamAuthTokenDebugSummaryDoesNotLeakToken(t *testing.T) {
	t.Parallel()

	token := "sk-test-secret-token"
	summary := NewAPIUpstreamAuthTokenDebugSummary(token)

	require.Contains(t, summary, "len=20")
	require.Contains(t, summary, "masked=sk-tes...oken")
	require.Contains(t, summary, "sha256_prefix=")
	require.NotContains(t, summary, token)
}

func TestResolveNewAPIUpstreamAuthTokenUsesSeparateAuthBaseURL(t *testing.T) {
	t.Parallel()

	apiServerHit := false
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiServerHit = true
		http.NotFound(w, r)
	}))
	defer apiServer.Close()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/user/login":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id": 42,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": []map[string]any{{"id": 7, "name": "jwell-upstream"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/7/key":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": "sk-upstream",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream","auth_base_url":"` + authServer.URL + `"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), apiServer.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-upstream", token)
	require.False(t, apiServerHit)
}

func TestResolveNewAPIUpstreamAuthTokenSupportsGetKeyFallback(t *testing.T) {
	t.Parallel()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/user/login":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id": 42,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": []map[string]any{{"id": 12, "name": "jwell-upstream"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/12/key":
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/12/key":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": "sk-get-fallback",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), authServer.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-get-fallback", token)
}

func TestResolveNewAPIUpstreamAuthTokenSupportsStringKeyData(t *testing.T) {
	t.Parallel()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/user/login":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id": 42,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": []map[string]any{{"id": 12, "name": "jwell-upstream"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/12/key":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data":    "sk-string-data",
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), authServer.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-string-data", token)
}

func TestInvalidateNewAPIUpstreamAuthToken(t *testing.T) {
	t.Parallel()

	keyFetchCount := 0
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/user/login":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id": 42,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/token/":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": []map[string]any{{"id": 7, "name": "jwell-upstream"}},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/7/key":
			keyFetchCount++
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": fmt.Sprintf("sk-%d", keyFetchCount),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), authServer.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-1", token)

	token, resolved, err = ResolveNewAPIUpstreamAuthToken(context.Background(), authServer.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-1", token)
	require.True(t, InvalidateNewAPIUpstreamAuthToken(authServer.URL, rawKey))

	token, resolved, err = ResolveNewAPIUpstreamAuthToken(context.Background(), authServer.URL, rawKey, "")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-2", token)
}

func TestParseNewAPIUpstreamAuthConfigGoogleProfileUsesEnv(t *testing.T) {
	t.Setenv("GOOGLE_API_CN_AUTH_BASE_URL", "https://google-api.cn")
	t.Setenv("GOOGLE_API_CN_USERNAME", "alice")
	t.Setenv("GOOGLE_API_CN_PASSWORD", "secret")
	t.Setenv("GOOGLE_API_CN_TOKEN_NAME", "google-upstream")
	t.Setenv("GOOGLE_API_CN_GROUP", "vip")

	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(`{"type":"newapi_login","profile":"google_api_cn"}`)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "alice", cfg.Username)
	require.Equal(t, "secret", cfg.Password)
	require.Equal(t, "https://google-api.cn", cfg.AuthBaseURL)
	require.Equal(t, "google-upstream", cfg.TokenName)
	require.Equal(t, "vip", cfg.Group)
}

func TestParseNewAPIUpstreamAuthConfigGoogleProfileDefaultsAuthBaseURL(t *testing.T) {
	t.Setenv("GOOGLE_API_CN_USERNAME", "alice")
	t.Setenv("GOOGLE_API_CN_PASSWORD", "secret")

	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(`{"type":"newapi_login","profile":"google_api_cn"}`)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "https://google-api.cn", cfg.AuthBaseURL)
}

func TestParseNewAPIUpstreamAuthConfigSupportsCustomEnvNames(t *testing.T) {
	t.Setenv("UPSTREAM_USERNAME", "bob")
	t.Setenv("UPSTREAM_PASSWORD", "hidden")

	rawKey := `{"type":"newapi_login","username_env":"UPSTREAM_USERNAME","password_env":"UPSTREAM_PASSWORD"}`
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "bob", cfg.Username)
	require.Equal(t, "hidden", cfg.Password)
	require.Equal(t, defaultNewAPIUpstreamTokenName, cfg.TokenName)
	require.Equal(t, defaultNewAPIUpstreamTokenGroup, cfg.Group)
}

func writeNewAPITestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	data, err := common.Marshal(v)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
}
