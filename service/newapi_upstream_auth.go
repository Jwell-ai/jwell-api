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
	"github.com/Jwell-ai/jwell-api/setting/operation_setting"
)

const (
	NewAPIUpstreamAuthType          = "newapi_login"
	defaultNewAPIUpstreamTokenName  = "jwell-api-upstream"
	defaultNewAPIUpstreamTokenGroup = "default"
	newAPIUpstreamTokenCacheTTL     = 30 * time.Minute
	newAPIUpstreamAuthDebugEnv      = "NEWAPI_UPSTREAM_AUTH_DEBUG"
	googleAPICNAuthDebugEnv         = "GOOGLE_API_CN_DEBUG_AUTH_TOKEN"
	newAPIUpstreamTokenRedisPrefix  = "newapi:upstream_auth:token"
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

	profile := strings.ToLower(strings.ReplaceAll(cfg.Profile, "-", "_"))
	explicitAuthBaseEnv := cfg.AuthBaseEnv != ""
	explicitUsernameEnv := cfg.UsernameEnv != ""
	explicitPasswordEnv := cfg.PasswordEnv != ""
	explicitTokenNameEnv := cfg.TokenNameEnv != ""
	explicitGroupEnv := cfg.GroupEnv != ""

	switch profile {
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

	if profile == "google_api_cn" {
		upstreamSetting := operation_setting.GetGoogleAPICNSetting()
		if cfg.AuthBaseURL == "" && !explicitAuthBaseEnv {
			cfg.AuthBaseURL = strings.TrimRight(strings.TrimSpace(upstreamSetting.AuthBaseURL), "/")
		}
		if cfg.Username == "" && !explicitUsernameEnv {
			cfg.Username = strings.TrimSpace(upstreamSetting.Username)
		}
		if cfg.Password == "" && !explicitPasswordEnv {
			cfg.Password = strings.TrimSpace(upstreamSetting.Password)
		}
		if cfg.TokenName == "" && !explicitTokenNameEnv {
			cfg.TokenName = strings.TrimSpace(upstreamSetting.TokenName)
		}
		if cfg.Group == "" && !explicitGroupEnv {
			cfg.Group = strings.TrimSpace(upstreamSetting.Group)
		}
	}

	if cfg.AuthBaseURL == "" && cfg.AuthBaseEnv != "" {
		cfg.AuthBaseURL = strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString(cfg.AuthBaseEnv, "")), "/")
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
	if profile == "google_api_cn" {
		if cfg.AuthBaseURL == "" {
			cfg.AuthBaseURL = "https://google-api.cn"
		}
	}
}

func ResolveNewAPIUpstreamAuthToken(ctx context.Context, baseURL string, rawKey string, proxy string) (string, bool, error) {
	return resolveNewAPIUpstreamAuthToken(ctx, baseURL, rawKey, proxy, "")
}

func ResolveNewAPIUpstreamAuthTokenForGroup(ctx context.Context, baseURL string, rawKey string, proxy string, group string) (string, bool, error) {
	return resolveNewAPIUpstreamAuthToken(ctx, baseURL, rawKey, proxy, group)
}

func EnsureNewAPIUpstreamAuthTokensForGroups(ctx context.Context, baseURL string, rawKey string, proxy string, groups []string) (int, bool, error) {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return 0, ok, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	authBaseURL := cfg.AuthBaseURL
	if authBaseURL == "" {
		authBaseURL = baseURL
	}
	if authBaseURL == "" {
		return 0, true, errors.New("newapi upstream auth requires auth_base_url or channel base_url")
	}

	client, err := newNewAPIUpstreamHTTPClient(proxy)
	if err != nil {
		return 0, true, err
	}
	userID, err := loginNewAPIUpstream(ctx, client, authBaseURL, cfg)
	if err != nil {
		return 0, true, err
	}
	if userID <= 0 {
		return 0, true, errors.New("newapi upstream login returned invalid user id")
	}

	syncedTokens, _, err := syncNewAPIUpstreamTokenCatalog(ctx, client, authBaseURL, userID, cfg)
	if err != nil {
		return 0, true, err
	}

	ensured := 0
	for _, group := range normalizeNewAPIUpstreamAuthGroups(groups, cfg.Group) {
		groupCfg := cfg
		applyNewAPIUpstreamAuthGroupOverride(&groupCfg, group)
		if !newAPIUpstreamTokenExists(syncedTokens, groupCfg) {
			common.SysLog(fmt.Sprintf("newapi upstream token prefetch skipped: token_name=%s group=%s not found", groupCfg.TokenName, groupCfg.Group))
			continue
		}
		token, _ := getNewAPIUpstreamTokenFromCatalog(syncedTokens, groupCfg)
		LogNewAPIUpstreamAuthTokenDebug("ensure", baseURL, authBaseURL, groupCfg, token)
		ensured++
	}
	return ensured, true, nil
}

func ResolveUpstreamAuthGroupForModel(settings dto.ChannelOtherSettings, modelName string, platformGroup string) string {
	modelName = strings.TrimSpace(modelName)
	platformGroup = strings.TrimSpace(platformGroup)
	modelGroups := make([]string, 0)
	if modelName != "" && len(settings.UpstreamModelGroups) > 0 {
		modelGroups = settings.UpstreamModelGroups[modelName]
	}
	mappedGroup := ""

	if platformGroup != "" && len(settings.UpstreamGroupMapping) > 0 {
		if upstreamGroup := strings.TrimSpace(settings.UpstreamGroupMapping[platformGroup]); upstreamGroup != "" {
			mappedGroup = upstreamGroup
			logNewAPIUpstreamAuthGroupDecision(modelName, platformGroup, mappedGroup, modelGroups, upstreamGroup, "platform_group_mapping")
			return upstreamGroup
		}
	}

	for _, group := range modelGroups {
		if group = strings.TrimSpace(group); group != "" {
			logNewAPIUpstreamAuthGroupDecision(modelName, platformGroup, mappedGroup, modelGroups, group, "model_metadata_fallback")
			return group
		}
	}
	logNewAPIUpstreamAuthGroupDecision(modelName, platformGroup, mappedGroup, modelGroups, "", "empty")
	return ""
}

func logNewAPIUpstreamAuthGroupDecision(modelName string, platformGroup string, mappedGroup string, modelGroups []string, selectedGroup string, reason string) {
	if !newAPIUpstreamAuthDebugEnabled() {
		return
	}
	normalizedModelGroups := make([]string, 0, len(modelGroups))
	for _, group := range modelGroups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		normalizedModelGroups = append(normalizedModelGroups, group)
	}
	common.SysLog(fmt.Sprintf(
		"newapi upstream auth group debug: model=%s platform_group=%s mapped_group=%s model_groups=%v selected_group=%s reason=%s",
		modelName,
		platformGroup,
		mappedGroup,
		normalizedModelGroups,
		selectedGroup,
		reason,
	))
}

func resolveNewAPIUpstreamAuthToken(ctx context.Context, baseURL string, rawKey string, proxy string, group string) (string, bool, error) {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return rawKey, ok, err
	}
	applyNewAPIUpstreamAuthGroupOverride(&cfg, group)
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

	if token, ok := getNewAPIUpstreamTokenFromRedis(authBaseURL, cfg); ok {
		setNewAPIUpstreamTokenMemoryCache(authBaseURL, cfg, token)
		LogNewAPIUpstreamAuthTokenDebug("redis", baseURL, authBaseURL, cfg, token)
		return token, true, nil
	}

	token, err := fetchNewAPIUpstreamToken(ctx, authBaseURL, cfg, proxy)
	if err == nil && token != "" {
		setNewAPIUpstreamTokenCache(authBaseURL, cfg, token)
	}
	if err != nil {
		return "", true, err
	}
	LogNewAPIUpstreamAuthTokenDebug("fetch", baseURL, authBaseURL, cfg, token)
	return token, true, nil
}

func applyNewAPIUpstreamAuthGroupOverride(cfg *NewAPIUpstreamAuthConfig, group string) {
	if cfg == nil {
		return
	}
	group = strings.TrimSpace(group)
	if group == "" || group == "auto" {
		return
	}
	cfg.Group = group
	if isGoogleAPICNUpstreamAuthProfile(cfg.Profile) {
		cfg.TokenName = group
	}
}

func isGoogleAPICNUpstreamAuthProfile(profile string) bool {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(profile), "-", "_")) == "google_api_cn"
}

func setNewAPIUpstreamTokenCache(authBaseURL string, cfg NewAPIUpstreamAuthConfig, token string) {
	setNewAPIUpstreamTokenMemoryCache(authBaseURL, cfg, token)
	setNewAPIUpstreamTokenRedisCache(authBaseURL, cfg, token)
}

func setNewAPIUpstreamTokenMemoryCache(authBaseURL string, cfg NewAPIUpstreamAuthConfig, token string) {
	if strings.TrimSpace(token) == "" {
		return
	}
	newAPIUpstreamTokenCacheMu.Lock()
	newAPIUpstreamTokenCache[newAPIUpstreamAuthCacheKey(authBaseURL, cfg)] = newAPIUpstreamTokenCacheItem{
		token:     token,
		expiresAt: time.Now().Add(newAPIUpstreamTokenCacheTTL),
	}
	newAPIUpstreamTokenCacheMu.Unlock()
}

func setNewAPIUpstreamTokenRedisCache(authBaseURL string, cfg NewAPIUpstreamAuthConfig, token string) {
	if strings.TrimSpace(token) == "" || !common.RedisEnabled || common.RDB == nil {
		return
	}
	if err := common.RedisHSetField(newAPIUpstreamAuthRedisKey(authBaseURL, cfg), newAPIUpstreamAuthRedisField(cfg.TokenName, cfg.Group), token); err != nil {
		common.SysError("set newapi upstream token redis cache failed: " + err.Error())
	}
}

func getNewAPIUpstreamTokenFromRedis(authBaseURL string, cfg NewAPIUpstreamAuthConfig) (string, bool) {
	if !common.RedisEnabled || common.RDB == nil {
		return "", false
	}

	key := newAPIUpstreamAuthRedisKey(authBaseURL, cfg)
	fields := []string{newAPIUpstreamAuthRedisField(cfg.TokenName, cfg.Group)}
	if cfg.Group == "" || cfg.Group == defaultNewAPIUpstreamTokenGroup {
		fallbackField := newAPIUpstreamAuthRedisField(cfg.TokenName, "")
		if fallbackField != fields[0] {
			fields = append(fields, fallbackField)
		}
	}

	for _, field := range fields {
		token, err := common.RedisHGetField(key, field)
		if err == nil && strings.TrimSpace(token) != "" {
			return token, true
		}
	}
	return "", false
}

func newAPIUpstreamTokenExists(tokens map[string]string, cfg NewAPIUpstreamAuthConfig) bool {
	_, ok := getNewAPIUpstreamTokenFromCatalog(tokens, cfg)
	return ok
}

func getNewAPIUpstreamTokenFromCatalog(tokens map[string]string, cfg NewAPIUpstreamAuthConfig) (string, bool) {
	if len(tokens) == 0 {
		return "", false
	}
	field := newAPIUpstreamAuthRedisField(cfg.TokenName, cfg.Group)
	if token := strings.TrimSpace(tokens[field]); token != "" {
		return token, true
	}
	if cfg.Group == "" || cfg.Group == defaultNewAPIUpstreamTokenGroup {
		field = newAPIUpstreamAuthRedisField(cfg.TokenName, "")
		if token := strings.TrimSpace(tokens[field]); token != "" {
			return token, true
		}
	}
	return "", false
}

func normalizeNewAPIUpstreamAuthGroups(groups []string, fallbackGroup string) []string {
	fallbackGroup = strings.TrimSpace(fallbackGroup)
	if fallbackGroup == "" || fallbackGroup == "auto" {
		fallbackGroup = defaultNewAPIUpstreamTokenGroup
	}
	result := make([]string, 0, len(groups)+1)
	seen := make(map[string]bool, len(groups)+1)
	add := func(group string) {
		group = strings.TrimSpace(group)
		if group == "" || group == "auto" || seen[group] {
			return
		}
		seen[group] = true
		result = append(result, group)
	}
	for _, group := range groups {
		add(group)
	}
	if len(result) == 0 {
		add(fallbackGroup)
	}
	return result
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
	if operation_setting.GetGoogleAPICNSetting().DebugAuthTokenFingerprint {
		return true
	}
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

func ClearNewAPIUpstreamTokenCache() int {
	newAPIUpstreamTokenCacheMu.Lock()
	defer newAPIUpstreamTokenCacheMu.Unlock()
	cleared := len(newAPIUpstreamTokenCache)
	newAPIUpstreamTokenCache = map[string]newAPIUpstreamTokenCacheItem{}
	return cleared
}

func invalidateNewAPIUpstreamAuthToken(baseURL string, rawKey string, group string) bool {
	cfg, ok, err := ParseNewAPIUpstreamAuthConfig(rawKey)
	if err != nil || !ok {
		return false
	}
	applyNewAPIUpstreamAuthGroupOverride(&cfg, group)
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	authBaseURL := cfg.AuthBaseURL
	if authBaseURL == "" {
		authBaseURL = baseURL
	}
	if authBaseURL == "" {
		return false
	}
	cacheKey := newAPIUpstreamAuthCacheKey(authBaseURL, cfg)
	invalidated := false
	newAPIUpstreamTokenCacheMu.Lock()
	if _, exists := newAPIUpstreamTokenCache[cacheKey]; exists {
		delete(newAPIUpstreamTokenCache, cacheKey)
		invalidated = true
	}
	if (cfg.Group == "" || cfg.Group == defaultNewAPIUpstreamTokenGroup) && cfg.Group != "" {
		fallbackCfg := cfg
		fallbackCfg.Group = ""
		fallbackCacheKey := newAPIUpstreamAuthCacheKey(authBaseURL, fallbackCfg)
		if _, exists := newAPIUpstreamTokenCache[fallbackCacheKey]; exists {
			delete(newAPIUpstreamTokenCache, fallbackCacheKey)
			invalidated = true
		}
	}
	newAPIUpstreamTokenCacheMu.Unlock()

	if !common.RedisEnabled || common.RDB == nil {
		return invalidated
	}

	fields := []string{newAPIUpstreamAuthRedisField(cfg.TokenName, cfg.Group)}
	if cfg.Group == "" || cfg.Group == defaultNewAPIUpstreamTokenGroup {
		fallbackField := newAPIUpstreamAuthRedisField(cfg.TokenName, "")
		if fallbackField != fields[0] {
			fields = append(fields, fallbackField)
		}
	}
	if err := common.RedisHDelField(newAPIUpstreamAuthRedisKey(authBaseURL, cfg), fields...); err != nil {
		common.SysError("invalidate newapi upstream token redis cache failed: " + err.Error())
		return invalidated
	}
	return invalidated || len(fields) > 0
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

func newAPIUpstreamAuthRedisKey(baseURL string, cfg NewAPIUpstreamAuthConfig) string {
	passwordHash := sha256.Sum256([]byte(cfg.Password))
	accountHash := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		strings.TrimSpace(cfg.Username),
		hex.EncodeToString(passwordHash[:]),
	}, "\x00")))
	return fmt.Sprintf("%s:%s", newAPIUpstreamTokenRedisPrefix, hex.EncodeToString(accountHash[:]))
}

func newAPIUpstreamAuthRedisField(tokenName string, group string) string {
	return strings.Join([]string{
		strings.TrimSpace(tokenName),
		strings.TrimSpace(group),
	}, "\x1f")
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

	tokens, items, err := syncNewAPIUpstreamTokenCatalog(ctx, client, baseURL, userID, cfg)
	if err != nil {
		return "", err
	}
	if token, ok := getNewAPIUpstreamTokenFromCatalog(tokens, cfg); ok {
		return token, nil
	}
	tokenID := findNewAPIUpstreamTokenInItems(items, cfg.TokenName, cfg.Group)
	if tokenID != 0 {
		token, err := getNewAPIUpstreamTokenKey(ctx, client, baseURL, userID, tokenID)
		if err != nil {
			return "", err
		}
		setNewAPIUpstreamTokenCache(baseURL, cfg, token)
		return token, nil
	}
	if cfg.Group != "" {
		return "", fmt.Errorf("newapi upstream token %q group %q not found", cfg.TokenName, cfg.Group)
	}
	return "", fmt.Errorf("newapi upstream token %q not found", cfg.TokenName)
}

func syncNewAPIUpstreamTokenCatalog(ctx context.Context, client *http.Client, baseURL string, userID int, cfg NewAPIUpstreamAuthConfig) (map[string]string, []newAPITokenItem, error) {
	items, err := listNewAPIUpstreamTokens(ctx, client, baseURL, userID)
	if err != nil {
		return nil, nil, err
	}
	tokens := make(map[string]string, len(items))
	for _, item := range items {
		token, err := getNewAPIUpstreamTokenKey(ctx, client, baseURL, userID, item.ID)
		if err != nil {
			common.SysError(fmt.Sprintf("newapi upstream token sync skipped: token_name=%s group=%s err=%s", item.Name, strings.TrimSpace(item.Group), err.Error()))
			continue
		}
		itemCfg := cfg
		itemCfg.TokenName = strings.TrimSpace(item.Name)
		itemCfg.Group = strings.TrimSpace(item.Group)
		field := newAPIUpstreamAuthRedisField(itemCfg.TokenName, itemCfg.Group)
		tokens[field] = token
		setNewAPIUpstreamTokenCache(baseURL, itemCfg, token)
	}
	return tokens, items, nil
}

func listNewAPIUpstreamTokens(ctx context.Context, client *http.Client, baseURL string, userID int) ([]newAPITokenItem, error) {
	var result newAPIResponse[newAPITokenPage]
	if err := doNewAPIJSON(ctx, client, http.MethodGet, baseURL+"/api/token/?p=1&size=100", userID, nil, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("newapi upstream token list failed: %s", result.Message)
	}
	return result.Data.Items, nil
}

func findNewAPIUpstreamToken(ctx context.Context, client *http.Client, baseURL string, userID int, tokenName string, group string) (int, error) {
	items, err := listNewAPIUpstreamTokens(ctx, client, baseURL, userID)
	if err != nil {
		return 0, err
	}
	return findNewAPIUpstreamTokenInItems(items, tokenName, group), nil
}

func findNewAPIUpstreamTokenInItems(items []newAPITokenItem, tokenName string, group string) int {
	var nameOnlyMatch int
	for _, item := range items {
		if item.Name != tokenName {
			continue
		}
		itemGroup := strings.TrimSpace(item.Group)
		if itemGroup == "" {
			if nameOnlyMatch == 0 && (group == "" || group == defaultNewAPIUpstreamTokenGroup) {
				nameOnlyMatch = item.ID
			}
			continue
		}
		if itemGroup == group {
			return item.ID
		}
	}
	if nameOnlyMatch != 0 {
		return nameOnlyMatch
	}
	return 0
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
