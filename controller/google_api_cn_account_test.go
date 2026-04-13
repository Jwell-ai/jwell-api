package controller

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/stretchr/testify/require"
)

func TestFetchGoogleAPICNUpstreamAccountUsesAuthBaseURL(t *testing.T) {
	t.Parallel()

	var apiServerHit atomic.Bool
	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiServerHit.Store(true)
		http.NotFound(w, r)
	}))
	defer apiServer.Close()

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/user/login":
			writeControllerTestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id": 42,
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/user/self":
			require.Equal(t, "42", r.Header.Get("New-Api-User"))
			writeControllerTestJSON(t, w, map[string]any{
				"success": true,
				"message": "",
				"data": map[string]any{
					"id":         42,
					"quota":      5000000,
					"used_quota": 1250000,
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	account, err := fetchGoogleAPICNUpstreamAccount(context.Background(), googleAPICNBootstrapConfig{
		BaseURL:     apiServer.URL,
		AuthBaseURL: authServer.URL,
	}, `{"type":"newapi_login","username":"alice","password":"secret","auth_base_url":"`+authServer.URL+`"}`, "")

	require.NoError(t, err)
	require.False(t, apiServerHit.Load())
	require.Equal(t, apiServer.URL, account.APIBaseURL)
	require.Equal(t, authServer.URL, account.AuthBaseURL)
	require.Equal(t, 12.5, account.TotalUSD)
	require.Equal(t, 2.5, account.UsedUSD)
	require.Equal(t, 10.0, account.BalanceUSD)
}

func writeControllerTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	data, err := common.Marshal(v)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
}
