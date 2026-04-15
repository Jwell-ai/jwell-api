package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/dto"
	"github.com/Jwell-ai/jwell-api/setting/operation_setting"
	"github.com/stretchr/testify/require"
)

func withGoogleAPICNSetting(t *testing.T, mutate func(*operation_setting.GoogleAPICNSetting)) {
	t.Helper()
	setting := operation_setting.GetGoogleAPICNSetting()
	original := *setting
	mutate(setting)
	t.Cleanup(func() {
		*setting = original
	})
}

func TestResolveNewAPIUpstreamAuthTokenFetchesExistingTokenAndCachesIt(t *testing.T) {
	t.Parallel()

	loginCount := 0
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
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": []map[string]any{{"id": 7, "name": "jwell-upstream"}},
				},
			})
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

func TestResolveNewAPIUpstreamAuthTokenReturnsErrorWhenTokenMissing(t *testing.T) {
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
					"items": []map[string]any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream","group":"default"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthToken(context.Background(), authServer.URL, rawKey, "")
	require.Error(t, err)
	require.True(t, resolved)
	require.Empty(t, token)
	require.Contains(t, err.Error(), `newapi upstream token "jwell-upstream" group "default" not found`)
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

func TestResolveNewAPIUpstreamAuthTokenForGroupUsesMatchingTokenGroup(t *testing.T) {
	t.Parallel()

	createdPayloadGroup := ""
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
					"items": []map[string]any{
						{"id": 7, "name": "jwell-upstream", "group": "default"},
						{"id": 8, "name": "jwell-upstream", "group": "vip"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/8/key":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": "sk-vip",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/":
			var payload map[string]any
			require.NoError(t, common.DecodeJson(r.Body, &payload))
			createdPayloadGroup = payload["group"].(string)
			writeNewAPITestJSON(t, w, map[string]any{"success": true, "message": "", "data": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream","group":"default"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthTokenForGroup(context.Background(), authServer.URL, rawKey, "", "vip")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-vip", token)
	require.Empty(t, createdPayloadGroup)
}

func TestResolveNewAPIUpstreamAuthTokenForGroupIgnoresNameOnlyTokenForDifferentGroup(t *testing.T) {
	t.Parallel()

	tokenListCalls := 0
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
			tokenListCalls++
			items := []map[string]any{{"id": 7, "name": "jwell-upstream"}}
			if tokenListCalls > 1 {
				items = append(items, map[string]any{"id": 8, "name": "jwell-upstream", "group": "vip"})
			}
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": items,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream","group":"default"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthTokenForGroup(context.Background(), authServer.URL, rawKey, "", "vip")
	require.Error(t, err)
	require.True(t, resolved)
	require.Empty(t, token)
	require.Contains(t, err.Error(), `newapi upstream token "jwell-upstream" group "vip" not found`)
}

func TestResolveGoogleAPICNUpstreamAuthTokenUsesGroupAsTokenName(t *testing.T) {
	t.Parallel()

	createdPayload := map[string]any{}
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
					"items": []map[string]any{
						{"id": 7, "name": "default", "group": "default"},
						{"id": 8, "name": "gemini-aistudio", "group": "gemini-aistudio"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/8/key":
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": "sk-gemini-aistudio",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/":
			require.NoError(t, common.DecodeJson(r.Body, &createdPayload))
			writeNewAPITestJSON(t, w, map[string]any{"success": true, "message": "", "data": map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","profile":"google_api_cn","username":"alice","password":"secret","token_name":"default","group":"default","auth_base_url":"` + authServer.URL + `"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthTokenForGroup(context.Background(), authServer.URL, rawKey, "", "gemini-aistudio")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-gemini-aistudio", token)
	require.Empty(t, createdPayload)
}

func TestEnsureNewAPIUpstreamAuthTokensForGroupsSkipsMissingGroups(t *testing.T) {
	loginCount := 0
	tokenIDByGroup := map[string]int{
		"default": 7,
	}
	groupByTokenID := map[int]string{
		7: "default",
	}
	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			items := make([]map[string]any, 0, len(tokenIDByGroup))
			for group, id := range tokenIDByGroup {
				items = append(items, map[string]any{"id": id, "name": "jwell-upstream", "group": group})
			}
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"items": items,
				},
			})
		default:
			var tokenID int
			if r.Method == http.MethodPost {
				_, _ = fmt.Sscanf(r.URL.Path, "/api/token/%d/key", &tokenID)
			}
			if group := groupByTokenID[tokenID]; tokenID > 0 && group != "" {
				writeNewAPITestJSON(t, w, map[string]any{
					"success": true,
					"message": "",
					"data": map[string]any{
						"key": "sk-" + group,
					},
				})
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream","group":"default"}`
	count, resolved, err := EnsureNewAPIUpstreamAuthTokensForGroups(context.Background(), authServer.URL, rawKey, "", []string{" default ", "vip", "vip", " "})
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, 1, count)
	require.Equal(t, 1, loginCount)

	token, resolved, err := ResolveNewAPIUpstreamAuthTokenForGroup(context.Background(), authServer.URL, rawKey, "", "vip")
	require.Error(t, err)
	require.True(t, resolved)
	require.Empty(t, token)
	require.Equal(t, 2, loginCount)
}

func TestResolveUpstreamAuthGroupForModelUsesProviderMetadataOnly(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		UpstreamModelGroups: map[string][]string{
			"gemini-2.5-pro": {"default"},
			"claude-sonnet":  {"vip", "default"},
		},
		UpstreamGroupMapping: map[string]string{
			"default": "default",
			"vip":     "vip",
			"svip":    "vip",
			"staff":   "not-model-group",
		},
	}

	require.Equal(t, "default", ResolveUpstreamAuthGroupForModel(settings, "gemini-2.5-pro", "vip"))
	require.Equal(t, "vip", ResolveUpstreamAuthGroupForModel(settings, "claude-sonnet", "svip"))
	require.Equal(t, "vip", ResolveUpstreamAuthGroupForModel(settings, "claude-sonnet", "staff"))
	require.Equal(t, "vip", ResolveUpstreamAuthGroupForModel(settings, "gpt-4o", "vip"))
	require.Empty(t, ResolveUpstreamAuthGroupForModel(settings, "gpt-4o", "unknown"))
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

func TestInvalidateNewAPIUpstreamAuthTokenForGroup(t *testing.T) {
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
					"items": []map[string]any{
						{"id": 7, "name": "jwell-upstream", "group": "default"},
						{"id": 8, "name": "jwell-upstream", "group": "vip"},
					},
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/token/8/key":
			keyFetchCount++
			writeNewAPITestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"key": fmt.Sprintf("sk-vip-%d", keyFetchCount),
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	rawKey := `{"type":"newapi_login","username":"alice","password":"secret","token_name":"jwell-upstream","group":"default"}`
	token, resolved, err := ResolveNewAPIUpstreamAuthTokenForGroup(context.Background(), authServer.URL, rawKey, "", "vip")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-vip-1", token)

	require.False(t, InvalidateNewAPIUpstreamAuthToken(authServer.URL, rawKey))
	require.True(t, InvalidateNewAPIUpstreamAuthTokenForGroup(authServer.URL, rawKey, "vip"))

	token, resolved, err = ResolveNewAPIUpstreamAuthTokenForGroup(context.Background(), authServer.URL, rawKey, "", "vip")
	require.NoError(t, err)
	require.True(t, resolved)
	require.Equal(t, "sk-vip-2", token)
}

func TestParseNewAPIUpstreamAuthConfigGoogleProfileUsesEnv(t *testing.T) {
	t.Setenv("GOOGLE_API_CN_AUTH_BASE_URL", "https://google-api.cn")
	t.Setenv("GOOGLE_API_CN_USERNAME", "alice")
	t.Setenv("GOOGLE_API_CN_PASSWORD", "secret")
	t.Setenv("GOOGLE_API_CN_TOKEN_NAME", "google-upstream")
	t.Setenv("GOOGLE_API_CN_GROUP", "vip")
	withGoogleAPICNSetting(t, func(setting *operation_setting.GoogleAPICNSetting) {
		setting.AuthBaseURL = ""
		setting.Username = ""
		setting.Password = ""
		setting.TokenName = ""
		setting.Group = ""
	})

	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(`{"type":"newapi_login","profile":"google_api_cn"}`)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "alice", cfg.Username)
	require.Equal(t, "secret", cfg.Password)
	require.Equal(t, "https://google-api.cn", cfg.AuthBaseURL)
	require.Equal(t, "google-upstream", cfg.TokenName)
	require.Equal(t, "vip", cfg.Group)
}

func TestParseNewAPIUpstreamAuthConfigGoogleProfilePrefersSettingOverEnv(t *testing.T) {
	t.Setenv("GOOGLE_API_CN_AUTH_BASE_URL", "https://env.example")
	t.Setenv("GOOGLE_API_CN_USERNAME", "env-user")
	t.Setenv("GOOGLE_API_CN_PASSWORD", "env-password")
	t.Setenv("GOOGLE_API_CN_TOKEN_NAME", "env-token")
	t.Setenv("GOOGLE_API_CN_GROUP", "env-group")
	withGoogleAPICNSetting(t, func(setting *operation_setting.GoogleAPICNSetting) {
		setting.AuthBaseURL = "https://setting.example"
		setting.Username = "setting-user"
		setting.Password = "setting-password"
		setting.TokenName = "setting-token"
		setting.Group = "setting-group"
	})

	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(`{"type":"newapi_login","profile":"google_api_cn"}`)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "setting-user", cfg.Username)
	require.Equal(t, "setting-password", cfg.Password)
	require.Equal(t, "https://setting.example", cfg.AuthBaseURL)
	require.Equal(t, "setting-token", cfg.TokenName)
	require.Equal(t, "setting-group", cfg.Group)
}

func TestParseNewAPIUpstreamAuthConfigGoogleProfileDefaultsAuthBaseURL(t *testing.T) {
	t.Setenv("GOOGLE_API_CN_USERNAME", "alice")
	t.Setenv("GOOGLE_API_CN_PASSWORD", "secret")
	withGoogleAPICNSetting(t, func(setting *operation_setting.GoogleAPICNSetting) {
		setting.AuthBaseURL = ""
		setting.Username = ""
		setting.Password = ""
		setting.TokenName = ""
		setting.Group = ""
	})

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
