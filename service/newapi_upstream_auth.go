package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"sync"
	"time"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/dto"
)

const (
	NewAPIUpstreamAuthType          = "newapi_login"
	defaultNewAPIUpstreamTokenName  = "jwell-api-upstream"
	defaultNewAPIUpstreamTokenGroup = "default"
	newAPIUpstreamTokenCacheTTL     = 30 * time.Minute
	newAPIUpstreamAuthDebugEnv      = "NEWAPI_UPSTREAM_AUTH_DEBUG"
	googleAPICNAuthDebugEnv         = "GOOGLE_API_CN_DEBUG_AUTH_TOKEN"
)

type NewAPIUpstreamAuthConfig struct {
	Type         string `json:"type"`
	Profile      string `json:"profile,omitempty"`
	AuthBaseURL  string `json:"auth_base_url,omitempty"`
	AuthBaseEnv  string `json:"auth_base_env,omitempty"`
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
	cfg.AuthBaseURL = strings.TrimRight(strings.TrimSpace(cfg.AuthBaseURL), "/")
	cfg.AuthBaseEnv = strings.TrimSpace(cfg.AuthBaseEnv)
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
		if cfg.AuthBaseEnv == "" {
			cfg.AuthBaseEnv = "GOOGLE_API_CN_AUTH_BASE_URL"
		}
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

	if cfg.AuthBaseURL == "" && cfg.AuthBaseEnv != "" {
		cfg.AuthBaseURL = strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString(cfg.AuthBaseEnv, "")), "/")
	}
	if cfg.AuthBaseURL == "" && strings.EqualFold(strings.ReplaceAll(cfg.Profile, "-", "_"), "google_api_cn") {
		cfg.AuthBaseURL = "https://google-api.cn"
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
	return resolveNewAPIUpstreamAuthToken(ctx, baseURL, rawKey, proxy, "")
}

func ResolveNewAPIUpstreamAuthTokenForGroup(ctx context.Context, baseURL string, rawKey string, proxy string, group string) (string, bool, error) {
	return resolveNewAPIUpstreamAuthToken(ctx, baseURL, rawKey, proxy, group)
}

func ResolveUpstreamAuthGroupForModel(settings dto.ChannelOtherSettings, modelName string, platformGroup string) string {
	modelName = strings.TrimSpace(modelName)
	platformGroup = strings.TrimSpace(platformGroup)
	modelGroups := make([]string, 0)
	if modelName != "" && len(settings.UpstreamModelGroups) > 0 {
		modelGroups = settings.UpstreamModelGroups[modelName]
	}

	if platformGroup != "" && len(settings.UpstreamGroupMapping) > 0 {
		if upstreamGroup := strings.TrimSpace(settings.UpstreamGroupMapping[platformGroup]); upstreamGroup != "" && upstreamGroupAllowedForModel(upstreamGroup, modelGroups) {
			return upstreamGroup
		}
	}

	for _, group := range modelGroups {
		if group = strings.TrimSpace(group); group != "" {
			return group
		}
	}
	return ""
}

func upstreamGroupAllowedForModel(group string, modelGroups []string) bool {
	group = strings.TrimSpace(group)
	if group == "" {
		return false
	}
	if len(modelGroups) == 0 {
		return true
	}
	for _, modelGroup := range modelGroups {
		if strings.TrimSpace(modelGroup) == group {
			return true
		}
	}
	return false
}

func resolveNewAPIUpstreamAuthToken(ctx context.Context, baseURL string, rawKey string, proxy string, group string) (string, bool, error) {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return rawKey, ok, err
	}
	if group = strings.TrimSpace(group); group != "" && group != "auto" {
		cfg.Group = group
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", true, errors.New("newapi upstream auth requires channel base_url")
	}
	authBaseURL := cfg.AuthBaseURL
	if authBaseURL == "" {
		authBaseURL = baseURL
	}

	cacheKey := newAPIUpstreamAuthCacheKey(authBaseURL, cfg)
	newAPIUpstreamTokenCacheMu.Lock()
	if cached, exists := newAPIUpstreamTokenCache[cacheKey]; exists && time.Now().Before(cached.expiresAt) && cached.token != "" {
		token := cached.token
		newAPIUpstreamTokenCacheMu.Unlock()
		LogNewAPIUpstreamAuthTokenDebug("cache", baseURL, authBaseURL, cfg, token)
		return token, true, nil
	}
	newAPIUpstreamTokenCacheMu.Unlock()

	token, err := fetchNewAPIUpstreamToken(ctx, authBaseURL, cfg, proxy)
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
	LogNewAPIUpstreamAuthTokenDebug("fetch", baseURL, authBaseURL, cfg, token)
	return token, true, nil
}

func LogNewAPIUpstreamAuthTokenDebug(source string, apiBaseURL string, authBaseURL string, cfg NewAPIUpstreamAuthConfig, token string) {
	if !newAPIUpstreamAuthDebugEnabled() {
		return
	}
	common.SysLog(fmt.Sprintf(
		"newapi upstream auth token debug: source=%s profile=%s api_base_url=%s auth_base_url=%s token_name=%s group=%s token=%s",
		source,
		cfg.Profile,
		strings.TrimRight(strings.TrimSpace(apiBaseURL), "/"),
		strings.TrimRight(strings.TrimSpace(authBaseURL), "/"),
		cfg.TokenName,
		cfg.Group,
		NewAPIUpstreamAuthTokenDebugSummary(token),
	))
}

func NewAPIUpstreamAuthTokenDebugSummary(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return "empty"
	}
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("len=%d masked=%s sha256_prefix=%s", len(token), maskNewAPIUpstreamAuthToken(token), hex.EncodeToString(sum[:])[:16])
}

func newAPIUpstreamAuthDebugEnabled() bool {
	return common.GetEnvOrDefaultBool(newAPIUpstreamAuthDebugEnv, false) ||
		common.GetEnvOrDefaultBool(googleAPICNAuthDebugEnv, false)
}

func maskNewAPIUpstreamAuthToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	if len(token) <= 12 {
		return strings.Repeat("*", len(token))
	}
	return token[:6] + "..." + token[len(token)-4:]
}

func InvalidateNewAPIUpstreamAuthToken(baseURL string, rawKey string) bool {
	return invalidateNewAPIUpstreamAuthToken(baseURL, rawKey, "")
}

func InvalidateNewAPIUpstreamAuthTokenForGroup(baseURL string, rawKey string, group string) bool {
	return invalidateNewAPIUpstreamAuthToken(baseURL, rawKey, group)
}

func invalidateNewAPIUpstreamAuthToken(baseURL string, rawKey string, group string) bool {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return false
	}
	if group = strings.TrimSpace(group); group != "" && group != "auto" {
		cfg.Group = group
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	authBaseURL := cfg.AuthBaseURL
	if authBaseURL == "" {
		authBaseURL = baseURL
	}
	if authBaseURL == "" {
		return false
	}
	cacheKey := newAPIUpstreamAuthCacheKey(authBaseURL, cfg)
	newAPIUpstreamTokenCacheMu.Lock()
	defer newAPIUpstreamTokenCacheMu.Unlock()
	if _, exists := newAPIUpstreamTokenCache[cacheKey]; !exists {
		return false
	}
	delete(newAPIUpstreamTokenCache, cacheKey)
	return true
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
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

type newAPITokenPage struct {
	Items []newAPITokenItem `json:"items"`
}

type newAPITokenKeyData struct {
	Key string `json:"key"`
}

type newAPITokenKeyResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Key     string          `json:"key,omitempty"`
	Data    json.RawMessage `json:"data"`
}

type NewAPIUpstreamAccount struct {
	UserID    int
	Quota     int
	UsedQuota int
}

type newAPIUserSelfData struct {
	ID        int `json:"id"`
	Quota     int `json:"quota"`
	UsedQuota int `json:"used_quota"`
}

func FetchNewAPIUpstreamAccount(ctx context.Context, rawKey string, proxy string) (NewAPIUpstreamAccount, bool, error) {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return NewAPIUpstreamAccount{}, ok, err
	}
	authBaseURL := cfg.AuthBaseURL
	if authBaseURL == "" {
		return NewAPIUpstreamAccount{}, true, errors.New("newapi upstream account requires auth_base_url")
	}
	client, err := newNewAPIUpstreamHTTPClient(proxy)
	if err != nil {
		return NewAPIUpstreamAccount{}, true, err
	}
	userID, err := loginNewAPIUpstream(ctx, client, authBaseURL, cfg)
	if err != nil {
		return NewAPIUpstreamAccount{}, true, err
	}
	if userID <= 0 {
		return NewAPIUpstreamAccount{}, true, errors.New("newapi upstream login returned invalid user id")
	}
	var result newAPIResponse[newAPIUserSelfData]
	if err = doNewAPIJSON(ctx, client, http.MethodGet, authBaseURL+"/api/user/self", userID, nil, &result); err != nil {
		return NewAPIUpstreamAccount{}, true, err
	}
	if !result.Success {
		return NewAPIUpstreamAccount{}, true, fmt.Errorf("newapi upstream user self failed: %s", result.Message)
	}
	if result.Data.ID == 0 {
		result.Data.ID = userID
	}
	return NewAPIUpstreamAccount{
		UserID:    result.Data.ID,
		Quota:     result.Data.Quota,
		UsedQuota: result.Data.UsedQuota,
	}, true, nil
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

	tokenID, err := findNewAPIUpstreamToken(ctx, client, baseURL, userID, cfg.TokenName, cfg.Group)
	if err != nil {
		return "", err
	}
	if tokenID == 0 {
		if err = createNewAPIUpstreamToken(ctx, client, baseURL, userID, cfg); err != nil {
			return "", err
		}
		tokenID, err = findNewAPIUpstreamToken(ctx, client, baseURL, userID, cfg.TokenName, cfg.Group)
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

func findNewAPIUpstreamToken(ctx context.Context, client *http.Client, baseURL string, userID int, tokenName string, group string) (int, error) {
	var result newAPIResponse[newAPITokenPage]
	if err := doNewAPIJSON(ctx, client, http.MethodGet, baseURL+"/api/token/?p=1&size=100", userID, nil, &result); err != nil {
		return 0, err
	}
	if !result.Success {
		return 0, fmt.Errorf("newapi upstream token list failed: %s", result.Message)
	}
	var nameOnlyMatch int
	for _, item := range result.Data.Items {
		if item.Name != tokenName {
			continue
		}
		itemGroup := strings.TrimSpace(item.Group)
		if itemGroup == "" {
			if nameOnlyMatch == 0 {
				nameOnlyMatch = item.ID
			}
			continue
		}
		if itemGroup == group {
			return item.ID, nil
		}
	}
	if nameOnlyMatch != 0 {
		return nameOnlyMatch, nil
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
	var result newAPITokenKeyResponse
	url := fmt.Sprintf("%s/api/token/%d/key", baseURL, tokenID)
	err := doNewAPIJSON(ctx, client, http.MethodPost, url, userID, nil, &result)
	if err != nil || !result.Success {
		postErr := err
		if postErr == nil {
			postErr = fmt.Errorf("newapi upstream token key fetch failed: %s", result.Message)
		}
		result = newAPITokenKeyResponse{}
		if getErr := doNewAPIJSON(ctx, client, http.MethodGet, url, userID, nil, &result); getErr != nil {
			return "", fmt.Errorf("%w; GET fallback failed: %w", postErr, getErr)
		}
	}
	if !result.Success {
		return "", fmt.Errorf("newapi upstream token key fetch failed: %s", result.Message)
	}
	key := parseNewAPIUpstreamTokenKey(result)
	if key == "" {
		return "", errors.New("newapi upstream token key is empty")
	}
	return key, nil
}

func parseNewAPIUpstreamTokenKey(result newAPITokenKeyResponse) string {
	if key := strings.TrimSpace(result.Key); key != "" {
		return key
	}
	if len(result.Data) == 0 {
		return ""
	}
	var keyData newAPITokenKeyData
	if err := common.Unmarshal(result.Data, &keyData); err == nil {
		if key := strings.TrimSpace(keyData.Key); key != "" {
			return key
		}
	}
	var rawKey string
	if err := common.Unmarshal(result.Data, &rawKey); err == nil {
		return strings.TrimSpace(rawKey)
	}
	return ""
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
