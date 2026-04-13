package operation_setting

import (
	"os"
	"strconv"

	"github.com/Jwell-ai/jwell-api/setting/config"
)

type GoogleAPICNSetting struct {
	Username                  string  `json:"username"`
	Password                  string  `json:"password"`
	TokenName                 string  `json:"token_name"`
	Group                     string  `json:"group"`
	GroupMapping              string  `json:"group_mapping"`
	AutoBootstrapEnabled      bool    `json:"auto_bootstrap_enabled"`
	AuthBaseURL               string  `json:"auth_base_url"`
	PricingURL                string  `json:"pricing_url"`
	APIBaseURL                string  `json:"api_base_url"`
	ChannelName               string  `json:"channel_name"`
	ChannelTag                string  `json:"channel_tag"`
	ChannelGroup              string  `json:"channel_group"`
	BootstrapModels           string  `json:"bootstrap_models"`
	AutoRegisterModelRatio    bool    `json:"auto_register_model_ratio_enabled"`
	DefaultModelRatio         float64 `json:"default_model_ratio"`
	BootstrapTimeoutSeconds   int     `json:"bootstrap_timeout_seconds"`
	DebugAuthTokenFingerprint bool    `json:"debug_auth_token"`
}

var googleAPICNSetting = GoogleAPICNSetting{
	Username:                  os.Getenv("GOOGLE_API_CN_USERNAME"),
	Password:                  os.Getenv("GOOGLE_API_CN_PASSWORD"),
	TokenName:                 getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_TOKEN_NAME", "jwell-api-upstream"),
	Group:                     getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_GROUP", "default"),
	GroupMapping:              os.Getenv("GOOGLE_API_CN_GROUP_MAPPING"),
	AutoBootstrapEnabled:      getGoogleAPICNEnvOrDefaultBool("GOOGLE_API_CN_AUTO_BOOTSTRAP_ENABLED", true),
	AuthBaseURL:               getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_AUTH_BASE_URL", "https://google-api.cn"),
	PricingURL:                getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_PRICING_URL", "https://google-api.cn/api/pricing"),
	APIBaseURL:                getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_API_BASE_URL", getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_BASE_URL", "https://gemini-api.cn")),
	ChannelName:               getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_CHANNEL_NAME", "google-api.cn"),
	ChannelTag:                getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_CHANNEL_TAG", "google-api-cn"),
	ChannelGroup:              getGoogleAPICNEnvOrDefault("GOOGLE_API_CN_CHANNEL_GROUP", "default"),
	BootstrapModels:           os.Getenv("GOOGLE_API_CN_BOOTSTRAP_MODELS"),
	AutoRegisterModelRatio:    getGoogleAPICNEnvOrDefaultBool("GOOGLE_API_CN_AUTO_REGISTER_MODEL_RATIO_ENABLED", true),
	DefaultModelRatio:         getGoogleAPICNEnvOrDefaultFloat("GOOGLE_API_CN_DEFAULT_MODEL_RATIO", 37.5),
	BootstrapTimeoutSeconds:   getGoogleAPICNEnvOrDefaultInt("GOOGLE_API_CN_BOOTSTRAP_TIMEOUT_SECONDS", 60),
	DebugAuthTokenFingerprint: getGoogleAPICNEnvOrDefaultBool("GOOGLE_API_CN_DEBUG_AUTH_TOKEN", false),
}

func init() {
	config.GlobalConfig.Register("google_api_cn", &googleAPICNSetting)
}

func GetGoogleAPICNSetting() *GoogleAPICNSetting {
	return &googleAPICNSetting
}

func getGoogleAPICNEnvOrDefault(key string, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getGoogleAPICNEnvOrDefaultBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getGoogleAPICNEnvOrDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}

func getGoogleAPICNEnvOrDefaultFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err == nil {
			return parsed
		}
	}
	return defaultValue
}
