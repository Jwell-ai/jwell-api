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
		pricingURL = authBaseURL + "/api/pricing"
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
	if err := ensureGoogleAPICNModelMetas(models); err != nil {
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
			if strings.TrimSpace(existingModel.Endpoints) == "" && endpoints != "" {
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
	case common.IsImageGenerationModel(name) ||
		strings.Contains(name, "image") ||
		strings.Contains(name, "imagen") ||
		strings.Contains(name, "nano-banana") ||
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
