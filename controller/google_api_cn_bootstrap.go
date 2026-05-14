package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/constant"
	"github.com/Jwell-ai/jwell-api/model"
	"github.com/Jwell-ai/jwell-api/service"
	"github.com/Jwell-ai/jwell-api/setting/operation_setting"
	"github.com/Jwell-ai/jwell-api/setting/ratio_setting"
	"gorm.io/gorm"
)

var (
	googleAPICNBootstrapScheduleMu    sync.Mutex
	googleAPICNBootstrapScheduleTimer *time.Timer
)

const (
	googleAPICNDefaultAPIBaseURL  = "https://gemini-api.cn"
	googleAPICNDefaultAuthBaseURL = "https://google-api.cn"
	googleAPICNDefaultName        = "google-api.cn"
	googleAPICNDefaultTag         = "google-api-cn"
	googleAPICNDefaultGroup       = "default"
	googleAPICNDefaultModelRatio  = 37.5
)

type googleAPICNBootstrapConfig struct {
	BaseURL                string
	AuthBaseURL            string
	PricingURL             string
	Name                   string
	Tag                    string
	Group                  string
	UpstreamTokenGroup     string
	UpstreamGroupMapping         map[string]string
	UpstreamGroupMappingExplicit bool // true when user explicitly configured the mapping (not a computed default)
	BootstrapModels        []string
	AutoRegisterModelRatio bool
	DefaultModelRatio      float64
}

type googleAPICNModelInfo struct {
	Name            string
	Groups          []string
	ModelRatio      float64 // 0 means not extracted from pricing data
	CompletionRatio float64 // 0 means not extracted
	ModelPrice      float64 // > 0 means fixed per-request price (not token-based)
}

// StartGoogleAPICNBootstrapTask creates or updates the shared google-api.cn
// upstream channel when the platform-level upstream account is configured.
func StartGoogleAPICNBootstrapTask() {
	if !common.IsMasterNode {
		return
	}
	upstreamSetting := operation_setting.GetGoogleAPICNSetting()
	if !upstreamSetting.AutoBootstrapEnabled {
		common.SysLog("google-api.cn bootstrap disabled by GOOGLE_API_CN_AUTO_BOOTSTRAP_ENABLED")
		return
	}

	cfg, ok := loadGoogleAPICNBootstrapConfig()
	if !ok {
		return
	}

	// Synchronously patch base_url for any matching channel that has it empty or
	// pointing at auth_base_url. This prevents "no base URL configured" errors
	// during the window while the full async bootstrap is still running.
	fastPatchGoogleAPICNChannelBaseURL(cfg)
	// Synchronously clear stale UpstreamGroupMapping so model_metadata_fallback
	// routes correctly from the first request, before the async bootstrap finishes.
	fastClearGoogleAPICNUpstreamGroupMapping(cfg)

	go func() {
		timeoutSeconds := upstreamSetting.BootstrapTimeoutSeconds
		if timeoutSeconds <= 0 {
			timeoutSeconds = 60
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
		defer cancel()

		if err := ensureGoogleAPICNChannel(ctx, cfg); err != nil {
			common.SysError("google-api.cn bootstrap failed: " + err.Error())
		}
	}()
}

// fastPatchGoogleAPICNChannelBaseURL synchronously ensures any channel identified
// by cfg.Tag has base_url set to cfg.BaseURL if it is currently empty or still
// pointing at the auth base URL. Called before the async bootstrap goroutine so
// relay requests don't fail with "no base URL configured" at startup.
func fastPatchGoogleAPICNChannelBaseURL(cfg googleAPICNBootstrapConfig) {
	if cfg.BaseURL == "" || cfg.Tag == "" {
		return
	}
	var channel model.Channel
	if err := model.DB.Where("tag = ?", cfg.Tag).Order("id asc").First(&channel).Error; err != nil {
		return // channel not yet created; the async bootstrap will create it
	}
	// Use the raw DB field, not GetBaseURL(), which now falls back to the type
	// default and would make the check always true for type-1 channels.
	rawURL := ""
	if channel.BaseURL != nil {
		rawURL = strings.TrimRight(strings.TrimSpace(*channel.BaseURL), "/")
	}
	authBase := strings.TrimRight(strings.TrimSpace(cfg.AuthBaseURL), "/")
	if rawURL != "" && rawURL != authBase {
		return // already has a non-default base URL
	}
	if err := model.DB.Model(&channel).Update("base_url", cfg.BaseURL).Error; err != nil {
		common.SysError(fmt.Sprintf("google-api.cn: fast base_url patch failed: %s", err.Error()))
		return
	}
	common.SysLog(fmt.Sprintf("google-api.cn: patched channel #%d base_url to %s", channel.Id, cfg.BaseURL))
	refreshChannelRuntimeCache()
}

// fastClearGoogleAPICNUpstreamGroupMapping synchronously removes any stale
// auto-synced UpstreamGroupMapping from the channel so that model_metadata_fallback
// routes correctly on the very first request after restart, before the async
// bootstrap goroutine completes.
func fastClearGoogleAPICNUpstreamGroupMapping(cfg googleAPICNBootstrapConfig) {
	if cfg.Tag == "" {
		return
	}
	var channel model.Channel
	if err := model.DB.Where("tag = ?", cfg.Tag).Order("id asc").First(&channel).Error; err != nil {
		return
	}
	settings := channel.GetOtherSettings()
	if len(settings.UpstreamGroupMapping) == 0 {
		return // already clear
	}
	settings.UpstreamGroupMapping = nil
	channel.SetOtherSettings(settings)
	if err := model.DB.Model(&channel).Update("settings", channel.OtherSettings).Error; err != nil {
		common.SysError(fmt.Sprintf("google-api.cn: failed to clear upstream group mapping: %s", err.Error()))
		return
	}
	common.SysLog(fmt.Sprintf("google-api.cn: cleared stale upstream group mapping on channel #%d", channel.Id))
	refreshChannelRuntimeCache()
}

func ScheduleGoogleAPICNBootstrapTask() {
	googleAPICNBootstrapScheduleMu.Lock()
	defer googleAPICNBootstrapScheduleMu.Unlock()

	if googleAPICNBootstrapScheduleTimer != nil {
		googleAPICNBootstrapScheduleTimer.Stop()
	}
	googleAPICNBootstrapScheduleTimer = time.AfterFunc(2*time.Second, StartGoogleAPICNBootstrapTask)
}

func loadGoogleAPICNBootstrapConfig() (googleAPICNBootstrapConfig, bool) {
	upstreamSetting := operation_setting.GetGoogleAPICNSetting()
	username := strings.TrimSpace(upstreamSetting.Username)
	password := strings.TrimSpace(upstreamSetting.Password)
	if username == "" || password == "" {
		return googleAPICNBootstrapConfig{}, false
	}

	baseURL := strings.TrimRight(strings.TrimSpace(upstreamSetting.APIBaseURL), "/")
	if baseURL == "" {
		baseURL = googleAPICNDefaultAPIBaseURL
	}
	authBaseURL := strings.TrimRight(strings.TrimSpace(upstreamSetting.AuthBaseURL), "/")
	if authBaseURL == "" {
		authBaseURL = googleAPICNDefaultAuthBaseURL
	}
	pricingURL := strings.TrimSpace(upstreamSetting.PricingURL)
	if pricingURL == "" {
		pricingURL = authBaseURL + "/api/pricing"
	}
	if strings.HasPrefix(pricingURL, "/") {
		pricingURL = authBaseURL + pricingURL
	}

	return normalizeGoogleAPICNBootstrapConfig(googleAPICNBootstrapConfig{
		BaseURL:                baseURL,
		AuthBaseURL:            authBaseURL,
		PricingURL:             strings.TrimRight(pricingURL, "/"),
		Name:                   strings.TrimSpace(upstreamSetting.ChannelName),
		Tag:                    strings.TrimSpace(upstreamSetting.ChannelTag),
		Group:                  strings.TrimSpace(upstreamSetting.ChannelGroup),
		UpstreamTokenGroup:     strings.TrimSpace(upstreamSetting.Group),
		UpstreamGroupMapping:         parseGoogleAPICNGroupMapping(upstreamSetting.GroupMapping),
		UpstreamGroupMappingExplicit: strings.TrimSpace(upstreamSetting.GroupMapping) != "",
		BootstrapModels:        normalizeModelNames(strings.Split(upstreamSetting.BootstrapModels, ",")),
		AutoRegisterModelRatio: upstreamSetting.AutoRegisterModelRatio,
		DefaultModelRatio:      normalizeGoogleAPICNDefaultModelRatio(upstreamSetting.DefaultModelRatio),
	}), true
}

func normalizeGoogleAPICNBootstrapConfig(cfg googleAPICNBootstrapConfig) googleAPICNBootstrapConfig {
	if cfg.Name == "" {
		cfg.Name = googleAPICNDefaultName
	}
	if cfg.Tag == "" {
		cfg.Tag = googleAPICNDefaultTag
	}
	if cfg.Group == "" {
		cfg.Group = googleAPICNDefaultGroup
	}
	if cfg.UpstreamTokenGroup == "" {
		cfg.UpstreamTokenGroup = googleAPICNDefaultGroup
	}
	if cfg.AuthBaseURL == "" {
		cfg.AuthBaseURL = googleAPICNDefaultAuthBaseURL
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = googleAPICNDefaultAPIBaseURL
	}
	if cfg.PricingURL == "" {
		cfg.PricingURL = cfg.AuthBaseURL + "/api/pricing"
	}
	if len(cfg.UpstreamGroupMapping) == 0 {
		cfg.UpstreamGroupMapping = map[string]string{
			cfg.Group: cfg.UpstreamTokenGroup,
		}
	}
	return cfg
}

func normalizeGoogleAPICNDefaultModelRatio(ratio float64) float64 {
	if ratio < 0 {
		common.SysError(fmt.Sprintf("invalid google-api.cn default model ratio: %.4f, using default value: %.2f", ratio, googleAPICNDefaultModelRatio))
		return googleAPICNDefaultModelRatio
	}
	return ratio
}

func ensureGoogleAPICNChannel(ctx context.Context, cfg googleAPICNBootstrapConfig) error {
	cfg = normalizeGoogleAPICNBootstrapConfig(cfg)

	key, err := googleAPICNChannelKey(cfg.AuthBaseURL)
	if err != nil {
		return err
	}

	var channel model.Channel
	err = model.DB.
		Where("tag = ? OR base_url = ? OR base_url = ?", cfg.Tag, cfg.BaseURL, cfg.AuthBaseURL).
		Order("id asc").
		First(&channel).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return createGoogleAPICNChannel(ctx, cfg, key)
	}

	return syncGoogleAPICNChannel(ctx, &channel, cfg, key)
}

func googleAPICNChannelKey(authBaseURL string) (string, error) {
	data, err := common.Marshal(service.NewAPIUpstreamAuthConfig{
		Type:        service.NewAPIUpstreamAuthType,
		Profile:     "google_api_cn",
		AuthBaseURL: authBaseURL,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func createGoogleAPICNChannel(ctx context.Context, cfg googleAPICNBootstrapConfig, key string) error {
	channel := model.Channel{
		Type:        constant.ChannelTypeOpenAI,
		Key:         key,
		Status:      common.ChannelStatusEnabled,
		Name:        cfg.Name,
		BaseURL:     common.GetPointer(cfg.BaseURL),
		Group:       cfg.Group,
		CreatedTime: common.GetTimestamp(),
		Priority:    common.GetPointer[int64](0),
		Weight:      common.GetPointer[uint](0),
		AutoBan:     common.GetPointer(1),
		Tag:         common.GetPointer(cfg.Tag),
	}
	setGoogleAPICNUpstreamModelSettings(&channel)
	setGoogleAPICNUpstreamGroupMapping(&channel, cfg.UpstreamGroupMapping)

	modelInfos, err := fetchGoogleAPICNModelInfos(ctx, &channel, cfg)
	if err != nil {
		modelInfos = googleAPICNModelInfosFromNames(cfg.BootstrapModels, cfg.UpstreamTokenGroup)
		if len(modelInfos) == 0 {
			return fmt.Errorf("fetch upstream models failed and GOOGLE_API_CN_BOOTSTRAP_MODELS is empty: %w", err)
		}
		common.SysError("google-api.cn model fetch failed, using GOOGLE_API_CN_BOOTSTRAP_MODELS: " + err.Error())
	}
	models := googleAPICNModelInfoNames(modelInfos)
	upstreamModelGroups := googleAPICNModelInfoGroups(modelInfos, cfg.UpstreamTokenGroup)
	setGoogleAPICNUpstreamModelGroups(&channel, upstreamModelGroups)
	channel.Models = strings.Join(models, ",")
	if err := ensureGoogleAPICNUpstreamAuthTokens(ctx, &channel, key, cfg); err != nil {
		return err
	}
	if err := ensureGoogleAPICNModelRatios(modelInfos, cfg); err != nil {
		return err
	}
	if err := ensureGoogleAPICNModelMetas(models); err != nil {
		return err
	}

	if err := channel.Insert(); err != nil {
		return err
	}
	refreshChannelRuntimeCache()
	model.RefreshPricing()
	common.SysLog(fmt.Sprintf("google-api.cn channel bootstrapped: channel_id=%d models=%d", channel.Id, len(models)))
	return nil
}

func syncGoogleAPICNChannel(ctx context.Context, channel *model.Channel, cfg googleAPICNBootstrapConfig, key string) error {
	if channel == nil {
		return nil
	}

	channelBaseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	shouldOwnChannelKey := channel.GetTag() == cfg.Tag ||
		channelBaseURL == cfg.BaseURL ||
		channelBaseURL == cfg.AuthBaseURL ||
		strings.TrimSpace(channel.Key) == ""
	if shouldOwnChannelKey {
		channel.Key = key
	}
	originTag := channel.GetTag()
	if channel.GetTag() == "" {
		channel.Tag = common.GetPointer(cfg.Tag)
	}
	if channel.GetTag() == cfg.Tag || channel.BaseURL == nil || strings.TrimSpace(channel.GetBaseURL()) == "" || strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/") == cfg.AuthBaseURL {
		channel.BaseURL = common.GetPointer(cfg.BaseURL)
	}
	if strings.TrimSpace(channel.Name) == "" {
		channel.Name = cfg.Name
	}
	setGoogleAPICNUpstreamModelSettings(channel)

	_, modelInfos, err := fetchGoogleAPICNPricingResultAndModelInfos(ctx, channel, cfg)
	if err != nil {
		modelInfos = googleAPICNModelInfosFromNames(cfg.BootstrapModels, cfg.UpstreamTokenGroup)
		if len(modelInfos) == 0 {
			return fmt.Errorf("fetch upstream models failed and GOOGLE_API_CN_BOOTSTRAP_MODELS is empty: %w", err)
		}
		common.SysError("google-api.cn model fetch failed, using GOOGLE_API_CN_BOOTSTRAP_MODELS: " + err.Error())
	}

	// UpstreamGroupMapping is for EXPLICIT redirects (e.g. "vip" → "gpt-image").
	// Do NOT auto-populate from usable_group — identity mappings like
	// {"default":"default"} would bypass model_metadata_fallback and lock every
	// default-group user to the "default" upstream token regardless of the model.
	if cfg.UpstreamGroupMappingExplicit {
		setGoogleAPICNUpstreamGroupMapping(channel, cfg.UpstreamGroupMapping)
	} else {
		// Clear any stale auto-synced identity mapping left over from previous runs.
		setGoogleAPICNUpstreamGroupMapping(channel, nil)
	}

	models := googleAPICNModelInfoNames(modelInfos)
	upstreamModelGroups := googleAPICNModelInfoGroups(modelInfos, cfg.UpstreamTokenGroup)
	// Use the upstream model list as the authoritative source.
	// mergeModelNames (union) would accumulate stale models indefinitely when the
	// upstream removes them; instead, fall back to the existing list only when the
	// upstream returned nothing (fetch failure already handled above).
	authoritative := models
	if len(authoritative) == 0 {
		authoritative = googleAPICNFilterModelNames(channel.GetModels())
	}
	mergedUpstreamModelGroups := googleAPICNMergeModelGroups(authoritative, upstreamModelGroups, cfg.UpstreamTokenGroup)
	setGoogleAPICNUpstreamModelGroups(channel, mergedUpstreamModelGroups)
	modelsChanged := strings.Join(normalizeModelNames(channel.GetModels()), ",") != strings.Join(authoritative, ",")
	channel.Models = strings.Join(authoritative, ",")
	if shouldOwnChannelKey {
		if err := ensureGoogleAPICNUpstreamAuthTokens(ctx, channel, key, cfg); err != nil {
			return err
		}
	}
	if err := ensureGoogleAPICNModelRatios(modelInfos, cfg); err != nil {
		return err
	}
	if err := ensureGoogleAPICNModelMetas(models); err != nil {
		return err
	}
	tagChanged := originTag != channel.GetTag()
	abilitiesChanged := modelsChanged || tagChanged

	updates := map[string]interface{}{
		"name":     channel.Name,
		"base_url": channel.GetBaseURL(),
		"settings": channel.OtherSettings,
	}
	if channel.GetTag() != "" {
		updates["tag"] = channel.GetTag()
	}
	if shouldOwnChannelKey {
		updates["key"] = channel.Key
	}
	if modelsChanged {
		updates["models"] = channel.Models
	}

	if err := model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error; err != nil {
		return err
	}
	if abilitiesChanged {
		if err := channel.UpdateAbilities(nil); err != nil {
			return err
		}
		model.RefreshPricing()
	}
	refreshChannelRuntimeCache()
	common.SysLog(fmt.Sprintf("google-api.cn channel synced: channel_id=%d fetched_models=%d models_changed=%t", channel.Id, len(models), modelsChanged))
	return nil
}

func setGoogleAPICNUpstreamModelSettings(channel *model.Channel) {
	settings := channel.GetOtherSettings()
	settings.UpstreamModelUpdateCheckEnabled = true
	settings.UpstreamModelUpdateAutoSyncEnabled = true
	channel.SetOtherSettings(settings)
}

func setGoogleAPICNUpstreamGroupMapping(channel *model.Channel, mapping map[string]string) {
	if channel == nil {
		return
	}
	settings := channel.GetOtherSettings()
	settings.UpstreamGroupMapping = mapping
	channel.SetOtherSettings(settings)
}

func setGoogleAPICNUpstreamModelGroups(channel *model.Channel, modelGroups map[string][]string) {
	if channel == nil {
		return
	}
	settings := channel.GetOtherSettings()
	settings.UpstreamModelGroups = modelGroups
	channel.SetOtherSettings(settings)
}

func ensureGoogleAPICNUpstreamAuthTokens(ctx context.Context, channel *model.Channel, key string, cfg googleAPICNBootstrapConfig) error {
	groups := googleAPICNMappedUpstreamTokenGroups(cfg)
	if len(groups) == 0 {
		return nil
	}
	count, resolved, err := service.EnsureNewAPIUpstreamAuthTokensForGroups(ctx, cfg.BaseURL, key, channel.GetSetting().Proxy, groups)
	if err != nil {
		return err
	}
	if resolved {
		common.SysLog(fmt.Sprintf("google-api.cn upstream auth tokens ensured: groups=%d", count))
	}
	return nil
}

func googleAPICNMappedUpstreamTokenGroups(cfg googleAPICNBootstrapConfig) []string {
	groups := make([]string, 0, len(cfg.UpstreamGroupMapping)+1)
	groups = append(groups, cfg.UpstreamTokenGroup)
	for _, upstreamGroup := range cfg.UpstreamGroupMapping {
		groups = append(groups, upstreamGroup)
	}
	return normalizeModelNames(groups)
}

func parseGoogleAPICNGroupMapping(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	mapping := make(map[string]string)
	if err := common.UnmarshalJsonStr(raw, &mapping); err != nil {
		common.SysError("failed to parse GOOGLE_API_CN_GROUP_MAPPING: " + err.Error())
		return nil
	}
	normalized := make(map[string]string, len(mapping))
	for platformGroup, upstreamGroup := range mapping {
		platformGroup = strings.TrimSpace(platformGroup)
		upstreamGroup = strings.TrimSpace(upstreamGroup)
		if platformGroup == "" || upstreamGroup == "" {
			continue
		}
		normalized[platformGroup] = upstreamGroup
	}
	return normalized
}

func syncGoogleAPICNChannelUpstreamGroupsFromPricing(ctx context.Context, channel *model.Channel) error {
	cfg, ok := loadGoogleAPICNBootstrapConfig()
	if !ok || !googleAPICNConfigMatchesChannel(channel, cfg) {
		return nil
	}
	modelInfos, err := fetchGoogleAPICNModelInfos(ctx, channel, cfg)
	if err != nil {
		return err
	}
	cleanedModels := googleAPICNFilterModelNames(channel.GetModels())
	modelsChanged := strings.Join(normalizeModelNames(channel.GetModels()), ",") != strings.Join(cleanedModels, ",")
	if modelsChanged {
		channel.Models = strings.Join(cleanedModels, ",")
	}
	upstreamModelGroups := googleAPICNMergeModelGroups(cleanedModels, googleAPICNModelInfoGroups(modelInfos, cfg.UpstreamTokenGroup), cfg.UpstreamTokenGroup)
	setGoogleAPICNUpstreamGroupMapping(channel, cfg.UpstreamGroupMapping)
	setGoogleAPICNUpstreamModelGroups(channel, upstreamModelGroups)
	updates := map[string]interface{}{
		"settings": channel.OtherSettings,
	}
	if modelsChanged {
		updates["models"] = channel.Models
	}
	return model.DB.Model(&model.Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}

func fetchGoogleAPICNModelInfos(ctx context.Context, channel *model.Channel, cfg googleAPICNBootstrapConfig) ([]googleAPICNModelInfo, error) {
	_, modelInfos, err := fetchGoogleAPICNPricingResultAndModelInfos(ctx, channel, cfg)
	return modelInfos, err
}

func fetchGoogleAPICNPricingResultAndModelInfos(ctx context.Context, channel *model.Channel, cfg googleAPICNBootstrapConfig) (googleAPICNPricingResult, []googleAPICNModelInfo, error) {
	if googleAPICNConfigMatchesChannel(channel, cfg) {
		result, err := fetchGoogleAPICNPricingResult(ctx, cfg, channel.GetSetting().Proxy)
		if err == nil {
			// Strip entries whose name matches a usable_group key — the upstream
			// sometimes lists token group names (e.g. "claude-aws", "gpt-image")
			// as model entries, but they are routing aliases, not real models.
			modelInfos := filterOutGroupNames(result.ModelInfos, result.UsableGroups)
			return result, modelInfos, nil
		}
		common.SysError("google-api.cn pricing model fetch failed, falling back to API models: " + err.Error())
	}

	models, err := fetchGoogleAPICNModels(ctx, channel)
	if err != nil {
		return googleAPICNPricingResult{}, nil, err
	}
	return googleAPICNPricingResult{}, googleAPICNModelInfosFromNames(models, cfg.UpstreamTokenGroup), nil
}

// filterOutGroupNames removes model infos whose name appears in the usableGroups
// map AND looks like a pure group identifier (no digits). Model names such as
// "gpt-image-2" or "claude-3-5-sonnet" contain digits and must not be filtered
// even when a same-named platform group exists on the upstream.
func filterOutGroupNames(infos []googleAPICNModelInfo, usableGroups map[string]string) []googleAPICNModelInfo {
	if len(usableGroups) == 0 {
		return infos
	}
	filtered := make([]googleAPICNModelInfo, 0, len(infos))
	for _, info := range infos {
		name := strings.TrimSpace(info.Name)
		if _, isGroup := usableGroups[name]; isGroup && !googleAPICNNameContainsDigit(name) {
			continue
		}
		filtered = append(filtered, info)
	}
	return filtered
}

func googleAPICNNameContainsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func googleAPICNModelInfosFromNames(models []string, fallbackGroup string) []googleAPICNModelInfo {
	names := normalizeModelNames(models)
	modelInfos := make([]googleAPICNModelInfo, 0, len(names))
	for _, name := range names {
		modelInfos = append(modelInfos, googleAPICNModelInfo{
			Name:   name,
			Groups: googleAPICNNormalizeGroups(nil, fallbackGroup),
		})
	}
	return modelInfos
}

func googleAPICNModelInfoNames(modelInfos []googleAPICNModelInfo) []string {
	names := make([]string, 0, len(modelInfos))
	for _, item := range modelInfos {
		names = append(names, item.Name)
	}
	return googleAPICNFilterModelNames(names)
}

func googleAPICNModelInfoGroups(modelInfos []googleAPICNModelInfo, fallbackGroup string) map[string][]string {
	modelGroups := make(map[string][]string, len(modelInfos))
	for _, item := range modelInfos {
		name := strings.TrimSpace(item.Name)
		if name == "" || googleAPICNLooksLikeMetadataModelName(name) {
			continue
		}
		modelGroups[name] = mergeModelNames(modelGroups[name], googleAPICNNormalizeGroups(item.Groups, fallbackGroup))
	}
	return modelGroups
}

func googleAPICNMergeModelGroups(models []string, modelGroups map[string][]string, fallbackGroup string) map[string][]string {
	merged := make(map[string][]string, len(models))
	for _, modelName := range googleAPICNFilterModelNames(models) {
		groups := modelGroups[modelName]
		if len(merged[modelName]) == 0 {
			merged[modelName] = googleAPICNNormalizeGroups(groups, fallbackGroup)
		}
	}
	return merged
}

func googleAPICNFilterModelNames(models []string) []string {
	names := normalizeModelNames(models)
	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if googleAPICNLooksLikeMetadataModelName(name) {
			continue
		}
		filtered = append(filtered, name)
	}
	return filtered
}

func googleAPICNLooksLikeMetadataModelName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	lower := strings.ToLower(name)
	upper := strings.ToUpper(name)
	switch upper {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	if strings.HasSuffix(lower, ".color") {
		return true
	}
	if strings.ContainsAny(name, " \t\r\n") {
		return true
	}
	if len(name) == 32 {
		for _, r := range name {
			if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
				return false
			}
		}
		return true
	}
	switch lower {
	case "openai", "azureopenai", "google", "gemini", "vertex", "vertexai",
		"anthropic", "claude", "aws", "bedrock", "cohere", "minimax",
		"jina", "cloudflare", "siliconflow", "ali", "alibaba", "dashscope",
		"zhipu", "moonshot", "kimi", "baidu", "tencent", "hunyuan",
		"volcengine", "byteplus", "deepseek", "mistral", "ollama",
		"perplexity", "xai", "grok", "helicone", "veniceai",
		"阿里巴巴", "讯飞":
		return true
	}
	return false
}

func googleAPICNNormalizeGroups(groups []string, fallbackGroup string) []string {
	normalized := normalizeModelNames(groups)
	if len(normalized) == 0 {
		fallbackGroup = strings.TrimSpace(fallbackGroup)
		if fallbackGroup == "" {
			fallbackGroup = googleAPICNDefaultGroup
		}
		normalized = []string{fallbackGroup}
	}
	sort.Strings(normalized)
	return normalized
}

// ensureGoogleAPICNModelRatios syncs model_ratio, completion_ratio and
// model_price from upstream pricing data. fullSync=true also removes stale
// entries that are no longer in the upstream model list; fullSync=false only
// adds/updates (safe for partial model lists from channel update paths).
func ensureGoogleAPICNModelRatios(modelInfos []googleAPICNModelInfo, cfg googleAPICNBootstrapConfig) error {
	return syncGoogleAPICNPricing(modelInfos, cfg, true)
}

func ensureGoogleAPICNModelRatiosPartial(modelInfos []googleAPICNModelInfo, cfg googleAPICNBootstrapConfig) error {
	return syncGoogleAPICNPricing(modelInfos, cfg, false)
}

func syncGoogleAPICNPricing(modelInfos []googleAPICNModelInfo, cfg googleAPICNBootstrapConfig, fullSync bool) error {
	if !cfg.AutoRegisterModelRatio || len(modelInfos) == 0 {
		return nil
	}

	// Build upstream key set (used for stale removal in full-sync mode).
	upstreamKeys := make(map[string]struct{}, len(modelInfos))
	for _, info := range modelInfos {
		if name := strings.TrimSpace(info.Name); name != "" {
			upstreamKeys[ratio_setting.FormatMatchingModelName(name)] = struct{}{}
		}
	}

	// --- ModelRatio ---
	modelRatios, ratioAdded := mergeGoogleAPICNModelRatios(
		ratio_setting.GetModelRatioCopy(),
		ratio_setting.GetModelPriceCopy(),
		modelInfos,
		cfg.DefaultModelRatio,
	)
	ratioRemoved := 0
	if fullSync {
		for key, ratio := range modelRatios {
			if _, ok := upstreamKeys[key]; !ok && ratio == googleAPICNDefaultModelRatio {
				delete(modelRatios, key)
				ratioRemoved++
			}
		}
	}

	// --- CompletionRatio ---
	completionRatios := ratio_setting.GetCompletionRatioCopy()
	crAdded, crRemoved := 0, 0
	for _, info := range modelInfos {
		if info.CompletionRatio <= 0 {
			continue
		}
		key := ratio_setting.FormatMatchingModelName(strings.TrimSpace(info.Name))
		if _, exists := completionRatios[key]; !exists {
			crAdded++
		}
		completionRatios[key] = info.CompletionRatio
	}
	if fullSync {
		for key := range completionRatios {
			if _, ok := upstreamKeys[key]; !ok {
				delete(completionRatios, key)
				crRemoved++
			}
		}
	}

	// --- ModelPrice ---
	modelPrices := ratio_setting.GetModelPriceCopy()
	priceAdded, priceRemoved := 0, 0
	ratioDeletedForPrice := 0
	for _, info := range modelInfos {
		if info.ModelPrice <= 0 {
			continue
		}
		key := ratio_setting.FormatMatchingModelName(strings.TrimSpace(info.Name))
		if _, exists := modelPrices[key]; !exists {
			priceAdded++
		}
		modelPrices[key] = info.ModelPrice
		// Model is price-based — remove from ratio map to avoid double-billing.
		if _, had := modelRatios[key]; had {
			delete(modelRatios, key)
			ratioDeletedForPrice++
		}
	}
	if fullSync {
		for key := range modelPrices {
			if _, ok := upstreamKeys[key]; !ok {
				delete(modelPrices, key)
				priceRemoved++
			}
		}
	}

	ratioChanged := ratioAdded+ratioRemoved+ratioDeletedForPrice > 0
	crChanged := crAdded+crRemoved > 0
	priceChanged := priceAdded+priceRemoved > 0

	if !ratioChanged && !crChanged && !priceChanged {
		return nil
	}

	if ratioChanged {
		if data, err := common.Marshal(modelRatios); err == nil {
			_ = model.UpdateOption("ModelRatio", string(data))
		}
	}
	if crChanged {
		if data, err := common.Marshal(completionRatios); err == nil {
			_ = model.UpdateOption("CompletionRatio", string(data))
		}
	}
	if priceChanged {
		if data, err := common.Marshal(modelPrices); err == nil {
			_ = model.UpdateOption("ModelPrice", string(data))
		}
	}
	ratio_setting.InvalidateExposedDataCache()
	common.SysLog(fmt.Sprintf(
		"google-api.cn pricing synced: ratio +%d/-%d/%d  completion +%d/-%d  price +%d/-%d",
		ratioAdded, ratioRemoved, ratioDeletedForPrice, crAdded, crRemoved, priceAdded, priceRemoved,
	))
	return nil
}

func ensureGoogleAPICNModelRatiosForChannel(channel *model.Channel, models []string) error {
	cfg, ok := loadGoogleAPICNBootstrapConfig()
	if !ok || !googleAPICNConfigMatchesChannel(channel, cfg) {
		return nil
	}
	// Use partial sync: this call has only the newly-discovered models, not the
	// full upstream list, so stale-entry removal would incorrectly delete ratios
	// for models not in this incremental update.
	return ensureGoogleAPICNModelRatiosPartial(googleAPICNModelInfosFromNames(models, cfg.UpstreamTokenGroup), cfg)
}

func ensureGoogleAPICNModelMetas(models []string) error {
	names := normalizeModelNames(models)
	if len(names) == 0 {
		return nil
	}

	var existing []model.Model
	if err := model.DB.
		Select("id", "model_name", "endpoints", "vendor_id", "sync_official").
		Where("model_name IN ?", names).
		Find(&existing).Error; err != nil {
		return err
	}
	existingByName := make(map[string]model.Model, len(existing))
	for _, item := range existing {
		existingByName[item.ModelName] = item
	}

	now := common.GetTimestamp()
	created := 0
	updated := 0
	vendorIDs := make(map[string]int)
	for _, name := range names {
		endpoints, err := googleAPICNModelEndpoints(name)
		if err != nil {
			return err
		}
		vendorID, err := googleAPICNModelVendorID(name, vendorIDs)
		if err != nil {
			return err
		}
		if existingModel, ok := existingByName[name]; ok {
			updates := map[string]interface{}{}
			if shouldUpdateGoogleAPICNModelEndpoints(existingModel, endpoints) {
				updates["endpoints"] = endpoints
			}
			if existingModel.VendorID == 0 && vendorID > 0 {
				updates["vendor_id"] = vendorID
			}
			if len(updates) == 0 {
				continue
			}
			updates["updated_time"] = now
			if err := model.DB.Model(&model.Model{}).Where("id = ?", existingModel.Id).Updates(updates).Error; err != nil {
				return err
			}
			updated++
			continue
		}
		modelMeta := &model.Model{
			ModelName:    name,
			VendorID:     vendorID,
			Endpoints:    endpoints,
			Status:       1,
			SyncOfficial: 0,
			NameRule:     model.NameRuleExact,
			CreatedTime:  now,
			UpdatedTime:  now,
		}
		if err := modelMeta.Insert(); err != nil {
			return err
		}
		created++
	}
	if created > 0 || updated > 0 {
		model.RefreshPricing()
		common.SysLog(fmt.Sprintf("google-api.cn model metadata synced: created=%d updated=%d", created, updated))
	}
	return nil
}

func shouldUpdateGoogleAPICNModelEndpoints(existingModel model.Model, endpoints string) bool {
	if strings.TrimSpace(endpoints) == "" {
		return false
	}
	if strings.TrimSpace(existingModel.Endpoints) == "" {
		return true
	}
	return existingModel.SyncOfficial == 0 && strings.TrimSpace(existingModel.Endpoints) != endpoints
}

func googleAPICNModelEndpoints(modelName string) (string, error) {
	endpointTypes := googleAPICNModelEndpointTypes(modelName)
	if len(endpointTypes) == 0 {
		return "", nil
	}
	endpoints := make(map[constant.EndpointType]common.EndpointInfo, len(endpointTypes))
	for _, endpointType := range endpointTypes {
		info, ok := common.GetDefaultEndpointInfo(endpointType)
		if !ok {
			continue
		}
		endpoints[endpointType] = info
	}
	if len(endpoints) == 0 {
		return "", nil
	}
	data, err := common.Marshal(endpoints)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func googleAPICNModelEndpointTypes(modelName string) []constant.EndpointType {
	name := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case name == "":
		return nil
	case strings.Contains(name, "rerank"):
		return []constant.EndpointType{constant.EndpointTypeJinaRerank}
	case strings.Contains(name, "embedding") ||
		strings.Contains(name, "embed") ||
		strings.HasPrefix(name, "m3e") ||
		strings.Contains(name, "bge-"):
		return []constant.EndpointType{constant.EndpointTypeEmbeddings}
	case strings.Contains(name, "sora") ||
		strings.Contains(name, "veo-") ||
		strings.Contains(name, "video") ||
		strings.Contains(name, "seedance"):
		return []constant.EndpointType{constant.EndpointTypeOpenAIVideo}
	case googleAPICNIsGeminiNativeImageModel(name):
		return []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
	case common.IsImageGenerationModel(name) ||
		strings.Contains(name, "image") ||
		strings.Contains(name, "imagen") ||
		strings.Contains(name, "seedream") ||
		strings.Contains(name, "jimeng"):
		return []constant.EndpointType{constant.EndpointTypeImageGeneration}
	case strings.Contains(name, "claude"):
		return []constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI}
	case strings.Contains(name, "gemini") || strings.Contains(name, "gemma"):
		return []constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI}
	case strings.Contains(name, "codex") || common.IsOpenAIResponseOnlyModel(name):
		return []constant.EndpointType{constant.EndpointTypeOpenAIResponse}
	default:
		return []constant.EndpointType{constant.EndpointTypeOpenAI}
	}
}

func googleAPICNIsGeminiNativeImageModel(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	return strings.Contains(name, "nano-banana") ||
		(strings.Contains(name, "gemini") && strings.Contains(name, "image"))
}

func googleAPICNModelVendorID(modelName string, cache map[string]int) (int, error) {
	vendorName, icon := googleAPICNModelVendor(modelName)
	if vendorName == "" {
		return 0, nil
	}
	if id, ok := cache[vendorName]; ok {
		return id, nil
	}
	var vendor model.Vendor
	err := model.DB.Where("name = ?", vendorName).First(&vendor).Error
	if err == nil {
		cache[vendorName] = vendor.Id
		return vendor.Id, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, err
	}
	vendor = model.Vendor{
		Name:   vendorName,
		Icon:   icon,
		Status: 1,
	}
	if err := vendor.Insert(); err != nil {
		return 0, err
	}
	cache[vendorName] = vendor.Id
	return vendor.Id, nil
}

func googleAPICNModelVendor(modelName string) (string, string) {
	name := strings.ToLower(strings.TrimSpace(modelName))
	switch {
	case name == "":
		return "", ""
	case strings.Contains(name, "claude"):
		return "Anthropic", "Claude.Color"
	case strings.Contains(name, "gemini") ||
		strings.Contains(name, "gemma") ||
		strings.Contains(name, "imagen") ||
		strings.Contains(name, "veo-") ||
		strings.Contains(name, "nano-banana"):
		return "Google", "Gemini.Color"
	case strings.Contains(name, "gpt") ||
		strings.Contains(name, "chatgpt") ||
		strings.HasPrefix(name, "o1") ||
		strings.HasPrefix(name, "o3") ||
		strings.HasPrefix(name, "o4") ||
		strings.Contains(name, "dall-e") ||
		strings.Contains(name, "whisper") ||
		strings.Contains(name, "tts") ||
		strings.Contains(name, "sora"):
		return "OpenAI", "OpenAI"
	case strings.Contains(name, "deepseek"):
		return "DeepSeek", "DeepSeek"
	case strings.Contains(name, "qwen") || strings.Contains(name, "qwq"):
		return "阿里巴巴", "Qwen.Color"
	case strings.Contains(name, "moonshot") || strings.Contains(name, "kimi"):
		return "Moonshot", "Moonshot"
	case strings.Contains(name, "glm") || strings.Contains(name, "chatglm"):
		return "智谱", "Zhipu.Color"
	case strings.Contains(name, "ernie") || strings.Contains(name, "bge-"):
		return "百度", "Wenxin.Color"
	case strings.Contains(name, "hunyuan"):
		return "腾讯", "Hunyuan.Color"
	case strings.Contains(name, "command"):
		return "Cohere", "Cohere.Color"
	case strings.Contains(name, "grok"):
		return "xAI", "XAI"
	case strings.Contains(name, "jina"):
		return "Jina", "Jina"
	case strings.Contains(name, "mistral") || strings.Contains(name, "mixtral"):
		return "Mistral", "Mistral.Color"
	case strings.Contains(name, "doubao") || strings.Contains(name, "seedream") || strings.Contains(name, "seedance"):
		return "字节跳动", "Doubao.Color"
	default:
		return "", ""
	}
}

func googleAPICNConfigMatchesChannel(channel *model.Channel, cfg googleAPICNBootstrapConfig) bool {
	if channel == nil {
		return false
	}
	channelBaseURL := strings.TrimRight(strings.TrimSpace(channel.GetBaseURL()), "/")
	return channel.GetTag() == cfg.Tag ||
		channelBaseURL == cfg.BaseURL ||
		channelBaseURL == cfg.AuthBaseURL
}

func mergeGoogleAPICNModelRatios(existingRatios map[string]float64, existingPrices map[string]float64, modelInfos []googleAPICNModelInfo, defaultRatio float64) (map[string]float64, int) {
	merged := make(map[string]float64, len(existingRatios)+len(modelInfos))
	for modelName, ratio := range existingRatios {
		merged[modelName] = ratio
	}
	added := 0
	for _, info := range modelInfos {
		name := strings.TrimSpace(info.Name)
		if name == "" {
			continue
		}
		ratioKey := ratio_setting.FormatMatchingModelName(name)
		if _, ok := existingPrices[ratioKey]; ok {
			continue
		}
		if _, ok := merged[ratioKey]; ok {
			continue
		}
		ratio := defaultRatio
		if info.ModelRatio > 0 {
			ratio = info.ModelRatio
		}
		merged[ratioKey] = ratio
		added++
	}
	return merged, added
}

func fetchGoogleAPICNModels(ctx context.Context, channel *model.Channel) ([]string, error) {
	cfg, ok := loadGoogleAPICNBootstrapConfig()
	if ok && googleAPICNConfigMatchesChannel(channel, cfg) {
		models, err := fetchGoogleAPICNPricingModels(ctx, cfg, channel.GetSetting().Proxy)
		if err == nil {
			return models, nil
		}
		common.SysError("google-api.cn pricing model fetch failed, falling back to API models: " + err.Error())
	}

	result := make(chan struct {
		models []string
		err    error
	}, 1)
	go func() {
		models, err := fetchChannelUpstreamModelIDs(channel)
		result <- struct {
			models []string
			err    error
		}{models: models, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-result:
		return normalizeModelNames(res.models), res.err
	}
}

func fetchGoogleAPICNPricingModels(ctx context.Context, cfg googleAPICNBootstrapConfig, proxy string) ([]string, error) {
	modelInfos, err := fetchGoogleAPICNPricingModelInfos(ctx, cfg, proxy)
	if err != nil {
		return nil, err
	}
	return googleAPICNModelInfoNames(modelInfos), nil
}

type googleAPICNPricingResult struct {
	ModelInfos   []googleAPICNModelInfo
	UsableGroups map[string]string // upstream group name → upstream group name (identity, from usable_group)
}

func fetchGoogleAPICNPricingResult(ctx context.Context, cfg googleAPICNBootstrapConfig, proxy string) (googleAPICNPricingResult, error) {
	if strings.TrimSpace(cfg.PricingURL) == "" {
		return googleAPICNPricingResult{}, errors.New("google-api.cn pricing url is empty")
	}
	body, err := getResponseBodyWithContext(ctx, http.MethodGet, cfg.PricingURL, proxy, http.Header{
		"Accept": []string{"application/json"},
	})
	if err != nil {
		return googleAPICNPricingResult{}, err
	}
	modelInfos, err := parseGoogleAPICNPricingModelInfos(body)
	if err != nil {
		return googleAPICNPricingResult{}, err
	}
	if len(modelInfos) == 0 {
		return googleAPICNPricingResult{}, fmt.Errorf("google-api.cn pricing returned no models: %s", cfg.PricingURL)
	}
	for i := range modelInfos {
		modelInfos[i].Groups = googleAPICNNormalizeGroups(modelInfos[i].Groups, cfg.UpstreamTokenGroup)
	}
	return googleAPICNPricingResult{
		ModelInfos:   modelInfos,
		UsableGroups: parseGoogleAPICNPricingUsableGroups(body),
	}, nil
}

func fetchGoogleAPICNPricingModelInfos(ctx context.Context, cfg googleAPICNBootstrapConfig, proxy string) ([]googleAPICNModelInfo, error) {
	result, err := fetchGoogleAPICNPricingResult(ctx, cfg, proxy)
	return result.ModelInfos, err
}

// parseGoogleAPICNPricingUsableGroups extracts the usable_group field from the
// upstream /api/pricing response (checked at root level and under "data") and
// returns an identity map of upstream group name → upstream group name.
func parseGoogleAPICNPricingUsableGroups(body []byte) map[string]string {
	var payload map[string]any
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil
	}
	// Try root-level usable_group first, then data.usable_group
	if groups := extractGoogleAPICNUsableGroupMap(payload["usable_group"]); len(groups) > 0 {
		return groups
	}
	if data, ok := payload["data"].(map[string]any); ok {
		if groups := extractGoogleAPICNUsableGroupMap(data["usable_group"]); len(groups) > 0 {
			return groups
		}
	}
	return nil
}

func extractGoogleAPICNUsableGroupMap(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]string, len(typed))
		for k := range typed {
			if k = strings.TrimSpace(k); k != "" {
				result[k] = k
			}
		}
		return result
	case []any:
		result := make(map[string]string, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					result[s] = s
				}
			}
		}
		return result
	}
	return nil
}

func parseGoogleAPICNPricingModels(body []byte) ([]string, error) {
	modelInfos, err := parseGoogleAPICNPricingModelInfos(body)
	if err != nil {
		return nil, err
	}
	return googleAPICNModelInfoNames(modelInfos), nil
}

func parseGoogleAPICNPricingModelInfos(body []byte) ([]googleAPICNModelInfo, error) {
	var payload any
	if err := common.Unmarshal(body, &payload); err != nil {
		limited := strings.TrimSpace(string(body))
		if len(limited) > 200 {
			limited = limited[:200]
		}
		return nil, fmt.Errorf("decode google-api.cn pricing response failed: %w; body: %s", err, limited)
	}
	return normalizeGoogleAPICNPricingModelInfos(collectGoogleAPICNPricingModelInfos(payload, nil)), nil
}

func collectGoogleAPICNPricingModelInfos(value any, inheritedGroups []string) []googleAPICNModelInfo {
	switch typed := value.(type) {
	case string:
		name := strings.TrimSpace(typed)
		if name == "" {
			return nil
		}
		return []googleAPICNModelInfo{{Name: name, Groups: inheritedGroups}}
	case []any:
		models := make([]googleAPICNModelInfo, 0, len(typed))
		for _, item := range typed {
			models = append(models, collectGoogleAPICNPricingModelInfos(item, inheritedGroups)...)
		}
		return models
	case map[string]any:
		if modelName, ok := googleAPICNPricingModelNameFromMap(typed); ok {
			groups := googleAPICNPricingModelGroupsFromMap(typed)
			if len(groups) == 0 {
				groups = inheritedGroups
			}
			info := googleAPICNModelInfo{Name: modelName, Groups: groups}
			info.ModelRatio, info.CompletionRatio, info.ModelPrice = googleAPICNPricingModelRatioFromMap(typed)
			return []googleAPICNModelInfo{info}
		}
		models := make([]googleAPICNModelInfo, 0)
		for key, nested := range typed {
			if !googleAPICNPricingMapKeyCanContainModels(key, nested) {
				continue
			}
			nextGroups := inheritedGroups
			if googleAPICNPricingMapKeyLooksLikeGroup(key) {
				nextGroups = mergeModelNames(nextGroups, []string{key})
			}
			models = append(models, collectGoogleAPICNPricingModelInfos(nested, nextGroups)...)
		}
		return models
	default:
		return nil
	}
}

// googleAPICNPricingModelRatioFromMap extracts model_ratio, completion_ratio and
// model_price from a pricing entry. Returns zeroes when fields are absent.
func googleAPICNPricingModelRatioFromMap(item map[string]any) (modelRatio, completionRatio, modelPrice float64) {
	for _, key := range []string{"model_ratio", "ratio", "input_ratio"} {
		if v, ok := item[key].(float64); ok && v > 0 {
			modelRatio = v
			break
		}
	}
	for _, key := range []string{"completion_ratio", "output_ratio"} {
		if v, ok := item[key].(float64); ok && v > 0 {
			completionRatio = v
			break
		}
	}
	for _, key := range []string{"model_price", "price", "fixed_price"} {
		if v, ok := item[key].(float64); ok && v > 0 {
			modelPrice = v
			break
		}
	}
	return
}

func googleAPICNPricingModelNameFromMap(item map[string]any) (string, bool) {
	for _, key := range []string{"model_name", "model", "id"} {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}

func googleAPICNPricingModelGroupsFromMap(item map[string]any) []string {
	groups := make([]string, 0)
	for _, key := range []string{
		"group",
		"groups",
		"enable_group",
		"enable_groups",
		"available_group",
		"available_groups",
		"model_group",
		"model_groups",
		"token_group",
		"token_groups",
	} {
		if value, ok := item[key]; ok {
			groups = mergeModelNames(groups, googleAPICNPricingGroupsFromValue(value))
		}
	}
	return normalizeModelNames(groups)
}

func googleAPICNPricingGroupsFromValue(value any) []string {
	switch typed := value.(type) {
	case string:
		return normalizeModelNames(strings.FieldsFunc(typed, func(r rune) bool {
			return r == ',' || r == ';' || r == '|'
		}))
	case []any:
		groups := make([]string, 0, len(typed))
		for _, item := range typed {
			groups = mergeModelNames(groups, googleAPICNPricingGroupsFromValue(item))
		}
		return groups
	case []string:
		return normalizeModelNames(typed)
	case map[string]any:
		groups := make([]string, 0, len(typed))
		for key, enabled := range typed {
			switch v := enabled.(type) {
			case bool:
				if v {
					groups = append(groups, key)
				}
			case string:
				if strings.TrimSpace(v) != "" && strings.TrimSpace(v) != "false" {
					groups = append(groups, key)
				}
			}
		}
		return normalizeModelNames(groups)
	default:
		return nil
	}
}

func googleAPICNPricingMapKeyCanContainModels(key string, value any) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if googleAPICNPricingMapKeyIsMetadata(key) {
		return false
	}
	switch key {
	case "success", "message", "error", "code", "vendor", "vendor_name", "provider", "provider_name",
		"display_name", "description", "object", "type", "tags", "price", "prices", "pricing", "model_price",
		"model_prices", "ratio", "created", "created_at", "updated", "updated_at":
		return false
	case "data", "items", "models", "list", "children", "model_list":
		return true
	}
	switch value.(type) {
	case []any, []string, map[string]any:
		return true
	case string:
		return googleAPICNPricingMapKeyLooksLikeGroup(key)
	default:
		return false
	}
}

func googleAPICNPricingMapKeyLooksLikeGroup(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	if googleAPICNPricingMapKeyIsMetadata(key) {
		return false
	}
	switch key {
	case "success", "message", "error", "code", "data", "items", "models", "list", "prices", "pricing",
		"model_prices", "providers", "provider", "provider_name", "vendor", "vendor_name", "display_name",
		"description", "object", "type", "tags", "price", "model_price", "ratio", "created", "created_at",
		"updated", "updated_at", "children", "model_list":
		return false
	}
	if _, err := strconv.Atoi(key); err == nil {
		return false
	}
	return !strings.ContainsAny(key, " \t\r\n/")
}

func googleAPICNPricingMapKeyIsMetadata(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "pricing_version", "version", "versions", "api_version", "api_versions",
		"endpoint", "endpoints", "endpoint_type", "endpoint_types",
		"path", "paths", "url", "urls", "uri", "uris", "route", "routes",
		"method", "methods", "http_method", "http_methods",
		"request_path", "request_paths", "request_method", "request_methods",
		"base_url", "base_urls", "api_base_url", "auth_base_url":
		return true
	default:
		return false
	}
}

func normalizeGoogleAPICNPricingModelInfos(modelInfos []googleAPICNModelInfo) []googleAPICNModelInfo {
	modelGroups := make(map[string][]string, len(modelInfos))
	names := make([]string, 0, len(modelInfos))
	for _, item := range modelInfos {
		name := strings.TrimSpace(item.Name)
		if name == "" || googleAPICNLooksLikeMetadataModelName(name) {
			continue
		}
		if _, ok := modelGroups[name]; !ok {
			names = append(names, name)
		}
		modelGroups[name] = mergeModelNames(modelGroups[name], item.Groups)
	}
	result := make([]googleAPICNModelInfo, 0, len(names))
	for _, name := range normalizeModelNames(names) {
		result = append(result, googleAPICNModelInfo{
			Name:   name,
			Groups: normalizeModelNames(modelGroups[name]),
		})
	}
	return result
}
