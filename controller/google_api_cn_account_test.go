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
		require.Equal(t, "Bearer sk-upstream", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/dashboard/billing/subscription":
			writeControllerTestJSON(t, w, map[string]any{
				"hard_limit_usd":     30.0,
				"has_payment_method": true,
				"access_until":       12345,
			})
		case "/v1/dashboard/billing/usage":
			require.NotEmpty(t, r.URL.Query().Get("start_date"))
			require.NotEmpty(t, r.URL.Query().Get("end_date"))
			writeControllerTestJSON(t, w, map[string]any{
				"total_usage": 250.0,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer authServer.Close()

	account, err := fetchGoogleAPICNUpstreamAccount(context.Background(), googleAPICNBootstrapConfig{
		BaseURL:     apiServer.URL,
		AuthBaseURL: authServer.URL,
	}, "sk-upstream", "")

	require.NoError(t, err)
	require.False(t, apiServerHit.Load())
	require.Equal(t, apiServer.URL, account.APIBaseURL)
	require.Equal(t, authServer.URL, account.AuthBaseURL)
	require.Equal(t, 30.0, account.TotalUSD)
	require.Equal(t, 2.5, account.UsedUSD)
	require.Equal(t, 27.5, account.BalanceUSD)
	require.Equal(t, int64(12345), account.AccessUntil)
}

func writeControllerTestJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	data, err := common.Marshal(v)
	require.NoError(t, err)
	_, err = w.Write(data)
	require.NoError(t, err)
}
