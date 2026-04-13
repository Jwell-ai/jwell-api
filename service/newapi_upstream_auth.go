package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/Jwell-ai/jwell-api/common"
)

const (
	NewAPIUpstreamAuthType          = "newapi_login"
	defaultNewAPIUpstreamTokenName  = "jwell-api-upstream"
	defaultNewAPIUpstreamTokenGroup = "default"
	newAPIUpstreamTokenCacheTTL     = 30 * time.Minute
)

type NewAPIUpstreamAuthConfig struct {
	Type         string `json:"type"`
	Profile      string `json:"profile,omitempty"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	UsernameEnv  string `json:"username_env,omitempty"`
	PasswordEnv  string `json:"password_env,omitempty"`
	TokenName    string `json:"token_name,omitempty"`
	TokenNameEnv string `json:"token_name_env,omitempty"`
	Group        string `json:"group,omitempty"`
	GroupEnv     string `json:"group_env,omitempty"`
}

type newAPIUpstreamTokenCacheItem struct {
	token     string
	expiresAt time.Time
}

var (
	newAPIUpstreamTokenCacheMu sync.Mutex
	newAPIUpstreamTokenCache   = map[string]newAPIUpstreamTokenCacheItem{}
)

func ParseNewAPIUpstreamAuthConfig(rawKey string) (NewAPIUpstreamAuthConfig, bool, error) {
	var cfg NewAPIUpstreamAuthConfig
	trimmed := strings.TrimSpace(rawKey)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return cfg, false, nil
	}
	if err := common.Unmarshal([]byte(trimmed), &cfg); err != nil {
		return cfg, false, err
	}
	if strings.TrimSpace(cfg.Type) != NewAPIUpstreamAuthType {
		return cfg, false, nil
	}
	applyNewAPIUpstreamAuthEnv(&cfg)
	if cfg.Username == "" || cfg.Password == "" {
		if cfg.UsernameEnv != "" || cfg.PasswordEnv != "" {
			return cfg, true, fmt.Errorf("newapi upstream auth requires username and password; configure env %s and %s", cfg.UsernameEnv, cfg.PasswordEnv)
		}
		return cfg, true, errors.New("newapi upstream auth requires username and password")
	}
	if cfg.TokenName == "" {
		cfg.TokenName = defaultNewAPIUpstreamTokenName
	}
	if cfg.Group == "" {
		cfg.Group = defaultNewAPIUpstreamTokenGroup
	}
	return cfg, true, nil
}

func applyNewAPIUpstreamAuthEnv(cfg *NewAPIUpstreamAuthConfig) {
	cfg.Profile = strings.TrimSpace(cfg.Profile)
	cfg.Username = strings.TrimSpace(cfg.Username)
	cfg.Password = strings.TrimSpace(cfg.Password)
	cfg.UsernameEnv = strings.TrimSpace(cfg.UsernameEnv)
	cfg.PasswordEnv = strings.TrimSpace(cfg.PasswordEnv)
	cfg.TokenName = strings.TrimSpace(cfg.TokenName)
	cfg.TokenNameEnv = strings.TrimSpace(cfg.TokenNameEnv)
	cfg.Group = strings.TrimSpace(cfg.Group)
	cfg.GroupEnv = strings.TrimSpace(cfg.GroupEnv)

	switch strings.ToLower(strings.ReplaceAll(cfg.Profile, "-", "_")) {
	case "google_api_cn":
		if cfg.UsernameEnv == "" {
			cfg.UsernameEnv = "GOOGLE_API_CN_USERNAME"
		}
		if cfg.PasswordEnv == "" {
			cfg.PasswordEnv = "GOOGLE_API_CN_PASSWORD"
		}
		if cfg.TokenNameEnv == "" {
			cfg.TokenNameEnv = "GOOGLE_API_CN_TOKEN_NAME"
		}
		if cfg.GroupEnv == "" {
			cfg.GroupEnv = "GOOGLE_API_CN_GROUP"
		}
	}

	if cfg.Username == "" && cfg.UsernameEnv != "" {
		cfg.Username = strings.TrimSpace(common.GetEnvOrDefaultString(cfg.UsernameEnv, ""))
	}
	if cfg.Password == "" && cfg.PasswordEnv != "" {
		cfg.Password = strings.TrimSpace(common.GetEnvOrDefaultString(cfg.PasswordEnv, ""))
	}
	if cfg.TokenName == "" && cfg.TokenNameEnv != "" {
		cfg.TokenName = strings.TrimSpace(common.GetEnvOrDefaultString(cfg.TokenNameEnv, ""))
	}
	if cfg.Group == "" && cfg.GroupEnv != "" {
		cfg.Group = strings.TrimSpace(common.GetEnvOrDefaultString(cfg.GroupEnv, ""))
	}
}

func ResolveNewAPIUpstreamAuthToken(ctx context.Context, baseURL string, rawKey string, proxy string) (string, bool, error) {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return rawKey, ok, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", true, errors.New("newapi upstream auth requires channel base_url")
	}

	cacheKey := newAPIUpstreamAuthCacheKey(baseURL, cfg)
	newAPIUpstreamTokenCacheMu.Lock()
	if cached, exists := newAPIUpstreamTokenCache[cacheKey]; exists && time.Now().Before(cached.expiresAt) && cached.token != "" {
		token := cached.token
		newAPIUpstreamTokenCacheMu.Unlock()
		return token, true, nil
	}
	newAPIUpstreamTokenCacheMu.Unlock()

	token, err := fetchNewAPIUpstreamToken(ctx, baseURL, cfg, proxy)
	if err == nil && token != "" {
		newAPIUpstreamTokenCacheMu.Lock()
		newAPIUpstreamTokenCache[cacheKey] = newAPIUpstreamTokenCacheItem{
			token:     token,
			expiresAt: time.Now().Add(newAPIUpstreamTokenCacheTTL),
		}
		newAPIUpstreamTokenCacheMu.Unlock()
	}
	if err != nil {
		return "", true, err
	}
	return token, true, nil
}

func newAPIUpstreamAuthCacheKey(baseURL string, cfg NewAPIUpstreamAuthConfig) string {
	passwordHash := sha256.Sum256([]byte(cfg.Password))
	return strings.Join([]string{
		baseURL,
		cfg.Username,
		cfg.TokenName,
		cfg.Group,
		hex.EncodeToString(passwordHash[:]),
	}, "\x00")
}

type newAPIResponse[T any] struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type newAPIUserLoginData struct {
	ID         int  `json:"id"`
	Require2FA bool `json:"require_2fa"`
}

type newAPITokenItem struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

type newAPITokenPage struct {
	Items []newAPITokenItem `json:"items"`
}

type newAPITokenKeyData struct {
	Key string `json:"key"`
}

func fetchNewAPIUpstreamToken(ctx context.Context, baseURL string, cfg NewAPIUpstreamAuthConfig, proxy string) (string, error) {
	client, err := newNewAPIUpstreamHTTPClient(proxy)
	if err != nil {
		return "", err
	}
	userID, err := loginNewAPIUpstream(ctx, client, baseURL, cfg)
	if err != nil {
		return "", err
	}
	if userID <= 0 {
		return "", errors.New("newapi upstream login returned invalid user id")
	}

	tokenID, err := findNewAPIUpstreamToken(ctx, client, baseURL, userID, cfg.TokenName)
	if err != nil {
		return "", err
	}
	if tokenID == 0 {
		if err = createNewAPIUpstreamToken(ctx, client, baseURL, userID, cfg); err != nil {
			return "", err
		}
		tokenID, err = findNewAPIUpstreamToken(ctx, client, baseURL, userID, cfg.TokenName)
		if err != nil {
			return "", err
		}
		if tokenID == 0 {
			return "", fmt.Errorf("newapi upstream token %q was not found after creation", cfg.TokenName)
		}
	}
	return getNewAPIUpstreamTokenKey(ctx, client, baseURL, userID, tokenID)
}

func newNewAPIUpstreamHTTPClient(proxy string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	baseClient, err := GetHttpClientWithProxy(strings.TrimSpace(proxy))
	if err != nil {
		return nil, err
	}
	if baseClient == nil {
		baseClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &http.Client{
		Transport:     baseClient.Transport,
		CheckRedirect: baseClient.CheckRedirect,
		Timeout:       baseClient.Timeout,
		Jar:           jar,
	}, nil
}

func loginNewAPIUpstream(ctx context.Context, client *http.Client, baseURL string, cfg NewAPIUpstreamAuthConfig) (int, error) {
	payload, err := common.Marshal(map[string]string{
		"username": cfg.Username,
		"password": cfg.Password,
	})
	if err != nil {
		return 0, err
	}
	var result newAPIResponse[newAPIUserLoginData]
	if err = doNewAPIJSON(ctx, client, http.MethodPost, baseURL+"/api/user/login", 0, payload, &result); err != nil {
		return 0, err
	}
	if !result.Success {
		return 0, fmt.Errorf("newapi upstream login failed: %s", result.Message)
	}
	if result.Data.Require2FA {
		return 0, errors.New("newapi upstream login requires 2FA")
	}
	return result.Data.ID, nil
}

func findNewAPIUpstreamToken(ctx context.Context, client *http.Client, baseURL string, userID int, tokenName string) (int, error) {
	var result newAPIResponse[newAPITokenPage]
	if err := doNewAPIJSON(ctx, client, http.MethodGet, baseURL+"/api/token/?p=1&size=100", userID, nil, &result); err != nil {
		return 0, err
	}
	if !result.Success {
		return 0, fmt.Errorf("newapi upstream token list failed: %s", result.Message)
	}
	for _, item := range result.Data.Items {
		if item.Name == tokenName {
			return item.ID, nil
		}
	}
	return 0, nil
}

func createNewAPIUpstreamToken(ctx context.Context, client *http.Client, baseURL string, userID int, cfg NewAPIUpstreamAuthConfig) error {
	payload, err := common.Marshal(map[string]any{
		"name":            cfg.TokenName,
		"expired_time":    -1,
		"remain_quota":    0,
		"unlimited_quota": true,
		"group":           cfg.Group,
	})
	if err != nil {
		return err
	}
	var result newAPIResponse[any]
	if err = doNewAPIJSON(ctx, client, http.MethodPost, baseURL+"/api/token/", userID, payload, &result); err != nil {
		return err
	}
	if !result.Success {
		return fmt.Errorf("newapi upstream token create failed: %s", result.Message)
	}
	return nil
}

func getNewAPIUpstreamTokenKey(ctx context.Context, client *http.Client, baseURL string, userID int, tokenID int) (string, error) {
	var result newAPIResponse[newAPITokenKeyData]
	url := fmt.Sprintf("%s/api/token/%d/key", baseURL, tokenID)
	if err := doNewAPIJSON(ctx, client, http.MethodPost, url, userID, nil, &result); err != nil {
		return "", err
	}
	if !result.Success {
		return "", fmt.Errorf("newapi upstream token key fetch failed: %s", result.Message)
	}
	key := strings.TrimSpace(result.Data.Key)
	if key == "" {
		return "", errors.New("newapi upstream token key is empty")
	}
	return key, nil
}

func doNewAPIJSON(ctx context.Context, client *http.Client, method string, url string, userID int, payload []byte, out any) error {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if userID > 0 {
		req.Header.Set("New-Api-User", fmt.Sprintf("%d", userID))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("newapi upstream %s %s returned %s: %s", method, url, resp.Status, strings.TrimSpace(string(bodyBytes)))
	}
	if out == nil {
		return nil
	}
	if err = common.DecodeJson(resp.Body, out); err != nil {
		return fmt.Errorf("decode newapi upstream response failed: %w", err)
	}
	return nil
}
