package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/constant"
	"github.com/Jwell-ai/jwell-api/model"
	"github.com/Jwell-ai/jwell-api/service"
	"github.com/Jwell-ai/jwell-api/setting/ratio_setting"
	"gorm.io/gorm"
)

const (
	googleAPICNDefaultAPIBaseURL  = "https://future-api.vodeshop.com"
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
	BootstrapModels        []string
	AutoRegisterModelRatio bool
	DefaultModelRatio      float64
}

// StartGoogleAPICNBootstrapTask creates or updates the shared google-api.cn
// upstream channel when the platform-level upstream account is configured.
func StartGoogleAPICNBootstrapTask() {
	if !common.IsMasterNode {
		return
	}
	if !common.GetEnvOrDefaultBool("GOOGLE_API_CN_AUTO_BOOTSTRAP_ENABLED", true) {
		common.SysLog("google-api.cn bootstrap disabled by GOOGLE_API_CN_AUTO_BOOTSTRAP_ENABLED")
		return
	}

	cfg, ok := loadGoogleAPICNBootstrapConfig()
	if !ok {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(common.GetEnvOrDefault("GOOGLE_API_CN_BOOTSTRAP_TIMEOUT_SECONDS", 60))*time.Second)
		defer cancel()

		if err := ensureGoogleAPICNChannel(ctx, cfg); err != nil {
			common.SysError("google-api.cn bootstrap failed: " + err.Error())
		}
	}()
}

func loadGoogleAPICNBootstrapConfig() (googleAPICNBootstrapConfig, bool) {
	username := strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_USERNAME", ""))
	password := strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_PASSWORD", ""))
	if username == "" || password == "" {
		return googleAPICNBootstrapConfig{}, false
	}

	baseURL := strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_API_BASE_URL", "")), "/")
	if baseURL == "" {
		baseURL = strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_BASE_URL", "")), "/")
	}
	if baseURL == "" {
		baseURL = googleAPICNDefaultAPIBaseURL
	}
	authBaseURL := strings.TrimRight(strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_AUTH_BASE_URL", googleAPICNDefaultAuthBaseURL)), "/")
	if authBaseURL == "" {
		authBaseURL = googleAPICNDefaultAuthBaseURL
	}
	pricingURL := strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_PRICING_URL", ""))
	if pricingURL == "" {
		pricingURL = authBaseURL + "/pricing"
	}
	if strings.HasPrefix(pricingURL, "/") {
		pricingURL = authBaseURL + pricingURL
	}

	return googleAPICNBootstrapConfig{
		BaseURL:         baseURL,
		AuthBaseURL:     authBaseURL,
		PricingURL:      strings.TrimRight(pricingURL, "/"),
		Name:            strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_CHANNEL_NAME", googleAPICNDefaultName)),
		Tag:             strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_CHANNEL_TAG", googleAPICNDefaultTag)),
		Group:           strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_CHANNEL_GROUP", googleAPICNDefaultGroup)),
		BootstrapModels: normalizeModelNames(strings.Split(common.GetEnvOrDefaultString("GOOGLE_API_CN_BOOTSTRAP_MODELS", ""), ",")),
		AutoRegisterModelRatio: common.GetEnvOrDefaultBool(
			"GOOGLE_API_CN_AUTO_REGISTER_MODEL_RATIO_ENABLED",
			true,
		),
		DefaultModelRatio: getGoogleAPICNDefaultModelRatio(),
	}, true
}

func getGoogleAPICNDefaultModelRatio() float64 {
	raw := strings.TrimSpace(common.GetEnvOrDefaultString("GOOGLE_API_CN_DEFAULT_MODEL_RATIO", ""))
	if raw == "" {
		return googleAPICNDefaultModelRatio
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil || ratio < 0 {
		common.SysError(fmt.Sprintf("failed to parse GOOGLE_API_CN_DEFAULT_MODEL_RATIO: %s, using default value: %.2f", raw, googleAPICNDefaultModelRatio))
		return googleAPICNDefaultModelRatio
	}
	return ratio
}

func ensureGoogleAPICNChannel(ctx context.Context, cfg googleAPICNBootstrapConfig) error {
	if cfg.Name == "" {
		cfg.Name = googleAPICNDefaultName
	}
	if cfg.Tag == "" {
		cfg.Tag = googleAPICNDefaultTag
	}
	if cfg.Group == "" {
		cfg.Group = googleAPICNDefaultGroup
	}
	if cfg.AuthBaseURL == "" {
		cfg.AuthBaseURL = googleAPICNDefaultAuthBaseURL
	}

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

	models, err := fetchGoogleAPICNModels(ctx, &channel)
	if err != nil {
		models = cfg.BootstrapModels
		if len(models) == 0 {
			return fmt.Errorf("fetch upstream models failed and GOOGLE_API_CN_BOOTSTRAP_MODELS is empty: %w", err)
		}
		common.SysError("google-api.cn model fetch failed, using GOOGLE_API_CN_BOOTSTRAP_MODELS: " + err.Error())
	}
	channel.Models = strings.Join(models, ",")
	if err := ensureGoogleAPICNModelRatios(models, cfg); err != nil {
		return err
	}

	if err := channel.Insert(); err != nil {
		return err
	}
	refreshChannelRuntimeCache()
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
	groupChanged := false
	if strings.TrimSpace(channel.Group) == "" {
		channel.Group = cfg.Group
		groupChanged = true
	}
	setGoogleAPICNUpstreamModelSettings(channel)

	models, err := fetchGoogleAPICNModels(ctx, channel)
	if err != nil {
		models = cfg.BootstrapModels
		if len(models) == 0 {
			return fmt.Errorf("fetch upstream models failed and GOOGLE_API_CN_BOOTSTRAP_MODELS is empty: %w", err)
		}
		common.SysError("google-api.cn model fetch failed, using GOOGLE_API_CN_BOOTSTRAP_MODELS: " + err.Error())
	}

	mergedModels := mergeModelNames(channel.GetModels(), models)
	modelsChanged := strings.Join(normalizeModelNames(channel.GetModels()), ",") != strings.Join(mergedModels, ",")
	channel.Models = strings.Join(mergedModels, ",")
	if err := ensureGoogleAPICNModelRatios(models, cfg); err != nil {
		return err
	}
	tagChanged := originTag != channel.GetTag()
	abilitiesChanged := modelsChanged || groupChanged || tagChanged

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
	if channel.Group != "" {
		updates["group"] = channel.Group
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

func ensureGoogleAPICNModelRatios(models []string, cfg googleAPICNBootstrapConfig) error {
	if !cfg.AutoRegisterModelRatio {
		return nil
	}
	modelRatios, added := mergeGoogleAPICNModelRatios(
		ratio_setting.GetModelRatioCopy(),
		ratio_setting.GetModelPriceCopy(),
		models,
		cfg.DefaultModelRatio,
	)
	if added == 0 {
		return nil
	}
	data, err := common.Marshal(modelRatios)
	if err != nil {
		return err
	}
	if err = model.UpdateOption("ModelRatio", string(data)); err != nil {
		return err
	}
	ratio_setting.InvalidateExposedDataCache()
	common.SysLog(fmt.Sprintf("google-api.cn model ratios registered: added=%d ratio=%.4f", added, cfg.DefaultModelRatio))
	return nil
}

func ensureGoogleAPICNModelRatiosForChannel(channel *model.Channel, models []string) error {
	cfg, ok := loadGoogleAPICNBootstrapConfig()
	if !ok || !googleAPICNConfigMatchesChannel(channel, cfg) {
		return nil
	}
	return ensureGoogleAPICNModelRatios(models, cfg)
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

func mergeGoogleAPICNModelRatios(existingRatios map[string]float64, existingPrices map[string]float64, models []string, defaultRatio float64) (map[string]float64, int) {
	merged := make(map[string]float64, len(existingRatios)+len(models))
	for modelName, ratio := range existingRatios {
		merged[modelName] = ratio
	}
	added := 0
	for _, modelName := range normalizeModelNames(models) {
		ratioKey := ratio_setting.FormatMatchingModelName(modelName)
		if _, ok := existingPrices[ratioKey]; ok {
			continue
		}
		if _, ok := merged[ratioKey]; ok {
			continue
		}
		merged[ratioKey] = defaultRatio
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
	if strings.TrimSpace(cfg.PricingURL) == "" {
		return nil, errors.New("google-api.cn pricing url is empty")
	}
	body, err := getResponseBodyWithContext(ctx, http.MethodGet, cfg.PricingURL, proxy, http.Header{
		"Accept": []string{"application/json"},
	})
	if err != nil {
		return nil, err
	}
	models, err := parseGoogleAPICNPricingModels(body)
	if err != nil {
		return nil, err
	}
	if len(models) == 0 {
		return nil, fmt.Errorf("google-api.cn pricing returned no models: %s", cfg.PricingURL)
	}
	return models, nil
}

func parseGoogleAPICNPricingModels(body []byte) ([]string, error) {
	var payload any
	if err := common.Unmarshal(body, &payload); err != nil {
		limited := strings.TrimSpace(string(body))
		if len(limited) > 200 {
			limited = limited[:200]
		}
		return nil, fmt.Errorf("decode google-api.cn pricing response failed: %w; body: %s", err, limited)
	}
	models := collectGoogleAPICNPricingModelNames(payload)
	return normalizeModelNames(models), nil
}

func collectGoogleAPICNPricingModelNames(value any) []string {
	switch typed := value.(type) {
	case []any:
		models := make([]string, 0, len(typed))
		for _, item := range typed {
			models = append(models, collectGoogleAPICNPricingModelNames(item)...)
		}
		return models
	case map[string]any:
		if modelName, ok := googleAPICNPricingModelNameFromMap(typed); ok {
			return []string{modelName}
		}
		models := make([]string, 0)
		for _, key := range []string{"data", "items", "models", "list"} {
			if nested, ok := typed[key]; ok {
				models = append(models, collectGoogleAPICNPricingModelNames(nested)...)
			}
		}
		return models
	default:
		return nil
	}
}

func googleAPICNPricingModelNameFromMap(item map[string]any) (string, bool) {
	for _, key := range []string{"model_name", "model", "id"} {
		if value, ok := item[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), true
		}
	}
	return "", false
}
