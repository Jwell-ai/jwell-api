package controller

import (
	"testing"

	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/constant"
	"github.com/Jwell-ai/jwell-api/dto"
	"github.com/Jwell-ai/jwell-api/model"
	"github.com/stretchr/testify/require"
)

func TestNormalizeModelNames(t *testing.T) {
	result := normalizeModelNames([]string{
		" gpt-4o ",
		"",
		"gpt-4o",
		"gpt-4.1",
		"   ",
	})

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
}

func TestMergeModelNames(t *testing.T) {
	result := mergeModelNames(
		[]string{"gpt-4o", "gpt-4.1"},
		[]string{"gpt-4.1", " gpt-4.1-mini ", "gpt-4o"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"}, result)
}

func TestMergeGoogleAPICNModelRatiosAddsOnlyMissingModels(t *testing.T) {
	result, added := mergeGoogleAPICNModelRatios(
		map[string]float64{
			"gpt-4o": 2,
		},
		map[string]float64{
			"priced-model": 0.1,
		},
		[]string{"gpt-4o", "priced-model", "new-upstream-model", " new-upstream-model "},
		37.5,
	)

	require.Equal(t, 1, added)
	require.Equal(t, 2.0, result["gpt-4o"])
	require.NotContains(t, result, "priced-model")
	require.Equal(t, 37.5, result["new-upstream-model"])
}

func TestParseGoogleAPICNPricingModels(t *testing.T) {
	body := []byte(`{
		"success": true,
		"data": [
			{"model_name": " gpt-4o "},
			{"model": "claude-3-5-sonnet", "group": "vip"},
			{"id": "gemini-2.5-pro", "groups": ["default", "gemini"]},
			{"vendor": "ignored"}
		]
	}`)

	models, err := parseGoogleAPICNPricingModels(body)

	require.NoError(t, err)
	require.Equal(t, []string{"gpt-4o", "claude-3-5-sonnet", "gemini-2.5-pro"}, models)

	modelInfos, err := parseGoogleAPICNPricingModelInfos(body)
	require.NoError(t, err)
	require.Equal(t, []googleAPICNModelInfo{
		{Name: "gpt-4o", Groups: []string{}},
		{Name: "claude-3-5-sonnet", Groups: []string{"vip"}},
		{Name: "gemini-2.5-pro", Groups: []string{"default", "gemini"}},
	}, modelInfos)
}

func TestParseGoogleAPICNPricingModelInfosInheritsGroupKeys(t *testing.T) {
	body := []byte(`{
		"success": true,
		"data": {
			"default": ["gpt-4o"],
			"vip": [{"model": "claude-3-5-sonnet"}],
			"1": ["ignored-channel-type"]
		}
	}`)

	modelInfos, err := parseGoogleAPICNPricingModelInfos(body)

	require.NoError(t, err)
	require.ElementsMatch(t, []googleAPICNModelInfo{
		{Name: "gpt-4o", Groups: []string{"default"}},
		{Name: "claude-3-5-sonnet", Groups: []string{"vip"}},
		{Name: "ignored-channel-type", Groups: []string{}},
	}, modelInfos)
}

func TestParseGoogleAPICNPricingModelInfosIgnoresMetadataStrings(t *testing.T) {
	body := []byte(`{
		"success": true,
		"pricing_version": "a42d372ccf0b5dd13ecf71203521f9d2",
		"data": {
			"models": [
				{
					"model": "nano-banana",
					"group": "gemini",
					"endpoints": {
						"openai": {"method": "POST", "path": "/v1/chat/completions"},
						"gemini": {"method": "POST", "path": "/v1beta/models/{model}:generateContent"}
					}
				}
			],
			"endpoints": ["/v1/messages"],
			"method": "POST"
		}
	}`)

	models, err := parseGoogleAPICNPricingModels(body)

	require.NoError(t, err)
	require.Equal(t, []string{"nano-banana"}, models)

	modelInfos, err := parseGoogleAPICNPricingModelInfos(body)
	require.NoError(t, err)
	require.ElementsMatch(t, []googleAPICNModelInfo{
		{Name: "nano-banana", Groups: []string{"gemini"}},
	}, modelInfos)
}

func TestParseGoogleAPICNPricingModelInfosIgnoresProviderDescriptors(t *testing.T) {
	body := []byte(`{
		"success": true,
		"data": {
			"providers": [
				{"id": "openai", "name": "OpenAI", "icon": "OpenAI"},
				{"id": "Google", "name": "Google", "icon": "Gemini.Color"},
				{"id": "SAP AI Core", "name": "SAP AI Core"}
			],
			"models": [
				{"model": "gemini-2.5-flash", "group": "gemini-aistudio"}
			]
		}
	}`)

	models, err := parseGoogleAPICNPricingModels(body)

	require.NoError(t, err)
	require.Equal(t, []string{"gemini-2.5-flash"}, models)

	modelInfos, err := parseGoogleAPICNPricingModelInfos(body)
	require.NoError(t, err)
	require.ElementsMatch(t, []googleAPICNModelInfo{
		{Name: "gemini-2.5-flash", Groups: []string{"gemini-aistudio"}},
	}, modelInfos)
}

func TestGoogleAPICNFilterModelNamesDropsMetadataArtifacts(t *testing.T) {
	result := googleAPICNFilterModelNames([]string{
		"nano-banana",
		"a42d372ccf0b5dd13ecf71203521f9d2",
		"/v1/messages",
		"POST",
		"/v1beta/models/{model}:generateContent",
		"/v1/chat/completions",
		"openai",
		"OpenAI",
		"Google",
		"Gemini.Color",
		"SAP AI Core",
		"VeniceAI",
	})

	require.Equal(t, []string{"nano-banana"}, result)
}

func TestParseGoogleAPICNGroupMapping(t *testing.T) {
	result := parseGoogleAPICNGroupMapping(`{"default":"default","vip":"pro"," ":"ignored","empty":" "}`)

	require.Equal(t, map[string]string{
		"default": "default",
		"vip":     "pro",
	}, result)
}

func TestNormalizeGoogleAPICNBootstrapConfigDefaultsGroupMapping(t *testing.T) {
	cfg := normalizeGoogleAPICNBootstrapConfig(googleAPICNBootstrapConfig{
		Group:              "platform-default",
		UpstreamTokenGroup: "upstream-default",
	})

	require.Equal(t, map[string]string{
		"platform-default": "upstream-default",
	}, cfg.UpstreamGroupMapping)
	require.Equal(t, googleAPICNDefaultAPIBaseURL, cfg.BaseURL)
	require.Equal(t, googleAPICNDefaultAuthBaseURL, cfg.AuthBaseURL)
	require.Equal(t, googleAPICNDefaultName, cfg.Name)
	require.Equal(t, googleAPICNDefaultTag, cfg.Tag)

	cfg = normalizeGoogleAPICNBootstrapConfig(googleAPICNBootstrapConfig{
		Group:              "platform-default",
		UpstreamTokenGroup: "upstream-default",
		UpstreamGroupMapping: map[string]string{
			"vip": "upstream-vip",
		},
	})

	require.Equal(t, map[string]string{
		"vip": "upstream-vip",
	}, cfg.UpstreamGroupMapping)
}

func TestGoogleAPICNMappedUpstreamTokenGroups(t *testing.T) {
	groups := googleAPICNMappedUpstreamTokenGroups(googleAPICNBootstrapConfig{
		UpstreamTokenGroup: " default ",
		UpstreamGroupMapping: map[string]string{
			"default": "default",
			"vip":     " pro ",
			"staff":   "vip",
		},
	})

	require.ElementsMatch(t, []string{"default", "pro", "vip"}, groups)
}

func TestGoogleAPICNModelEndpointTypes(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeGemini, constant.EndpointTypeOpenAI},
		googleAPICNModelEndpointTypes("gemini-2.5-flash-thinking"),
	)
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeAnthropic, constant.EndpointTypeOpenAI},
		googleAPICNModelEndpointTypes("claude-opus-4-1-20250805"),
	)
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeImageGeneration},
		googleAPICNModelEndpointTypes("nano-banana-pro-preview"),
	)
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeEmbeddings},
		googleAPICNModelEndpointTypes("gemini-embedding-001"),
	)
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIResponse},
		googleAPICNModelEndpointTypes("gpt-5-codex"),
	)
}

func TestGoogleAPICNModelEndpointsUsesDefaultPaths(t *testing.T) {
	endpointsJSON, err := googleAPICNModelEndpoints("gemini-2.5-flash")
	require.NoError(t, err)

	var endpoints map[string]common.EndpointInfo
	require.NoError(t, common.UnmarshalJsonStr(endpointsJSON, &endpoints))
	require.Equal(t, "/v1beta/models/{model}:generateContent", endpoints[string(constant.EndpointTypeGemini)].Path)
	require.Equal(t, "/v1/chat/completions", endpoints[string(constant.EndpointTypeOpenAI)].Path)

	endpointsJSON, err = googleAPICNModelEndpoints("sora-2")
	require.NoError(t, err)
	require.NoError(t, common.UnmarshalJsonStr(endpointsJSON, &endpoints))
	require.Equal(t, "/v1/videos", endpoints[string(constant.EndpointTypeOpenAIVideo)].Path)
}

func TestSubtractModelNames(t *testing.T) {
	result := subtractModelNames(
		[]string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"},
		[]string{"gpt-4.1", "not-exists"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1-mini"}, result)
}

func TestIntersectModelNames(t *testing.T) {
	result := intersectModelNames(
		[]string{"gpt-4o", "gpt-4.1", "gpt-4.1", "not-exists"},
		[]string{"gpt-4.1", "gpt-4o-mini", "gpt-4o"},
	)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
}

func TestApplySelectedModelChanges(t *testing.T) {
	t.Run("add and remove together", func(t *testing.T) {
		result := applySelectedModelChanges(
			[]string{"gpt-4o", "gpt-4.1", "claude-3"},
			[]string{"gpt-4.1-mini"},
			[]string{"claude-3"},
		)

		require.Equal(t, []string{"gpt-4o", "gpt-4.1", "gpt-4.1-mini"}, result)
	})

	t.Run("add wins when conflict with remove", func(t *testing.T) {
		result := applySelectedModelChanges(
			[]string{"gpt-4o"},
			[]string{"gpt-4.1"},
			[]string{"gpt-4.1"},
		)

		require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, result)
	})
}

func TestCollectPendingApplyUpstreamModelChanges(t *testing.T) {
	settings := dto.ChannelOtherSettings{
		UpstreamModelUpdateLastDetectedModels: []string{" gpt-4o ", "gpt-4o", "gpt-4.1"},
		UpstreamModelUpdateLastRemovedModels:  []string{" old-model ", "", "old-model"},
	}

	pendingAddModels, pendingRemoveModels := collectPendingApplyUpstreamModelChanges(settings)

	require.Equal(t, []string{"gpt-4o", "gpt-4.1"}, pendingAddModels)
	require.Equal(t, []string{"old-model"}, pendingRemoveModels)
}

func TestNormalizeChannelModelMapping(t *testing.T) {
	modelMapping := `{
		" alias-model ": " upstream-model ",
		"": "invalid",
		"invalid-target": ""
	}`
	channel := &model.Channel{
		ModelMapping: &modelMapping,
	}

	result := normalizeChannelModelMapping(channel)
	require.Equal(t, map[string]string{
		"alias-model": "upstream-model",
	}, result)
}

func TestCollectPendingUpstreamModelChangesFromModels_WithModelMapping(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
		[]string{"alias-model", "gpt-4o", "stale-model"},
		[]string{"gpt-4o", "gpt-4.1", "mapped-target"},
		[]string{"gpt-4.1"},
		map[string]string{
			"alias-model": "mapped-target",
		},
	)

	require.Equal(t, []string{}, pendingAddModels)
	require.Equal(t, []string{"stale-model"}, pendingRemoveModels)
}

func TestCollectPendingUpstreamModelChangesFromModels_WithIgnoredRegexPatterns(t *testing.T) {
	pendingAddModels, pendingRemoveModels := collectPendingUpstreamModelChangesFromModels(
		[]string{"gpt-4o"},
		[]string{"gpt-4o", "claude-3-5-sonnet", "sora-video", "gpt-4.1"},
		[]string{"regex:^sora-.*$", "gpt-4.1"},
		nil,
	)

	require.Equal(t, []string{"claude-3-5-sonnet"}, pendingAddModels)
	require.Equal(t, []string{}, pendingRemoveModels)
}

func TestBuildUpstreamModelUpdateTaskNotificationContent_OmitOverflowDetails(t *testing.T) {
	channelSummaries := make([]upstreamModelUpdateChannelSummary, 0, 12)
	for i := 0; i < 12; i++ {
		channelSummaries = append(channelSummaries, upstreamModelUpdateChannelSummary{
			ChannelName: "channel-" + string(rune('A'+i)),
			AddCount:    i + 1,
			RemoveCount: i,
		})
	}

	content := buildUpstreamModelUpdateTaskNotificationContent(
		24,
		12,
		56,
		21,
		9,
		[]int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		channelSummaries,
		[]string{
			"gpt-4.1", "gpt-4.1-mini", "o3", "o4-mini", "gemini-2.5-pro", "claude-3.7-sonnet",
			"qwen-max", "deepseek-r1", "llama-3.3-70b", "mistral-large", "command-r-plus", "doubao-pro-32k",
			"hunyuan-large",
		},
		[]string{
			"gpt-3.5-turbo", "claude-2.1", "gemini-1.5-pro", "mixtral-8x7b", "qwen-plus", "glm-4",
			"yi-large", "moonshot-v1", "doubao-lite",
		},
	)

	require.Contains(t, content, "其余 4 个渠道已省略")
	require.Contains(t, content, "其余 1 个已省略")
	require.Contains(t, content, "失败渠道 ID（展示 10/12）")
	require.Contains(t, content, "其余 2 个已省略")
}

func TestShouldSendUpstreamModelUpdateNotification(t *testing.T) {
	channelUpstreamModelUpdateNotifyState.Lock()
	channelUpstreamModelUpdateNotifyState.lastNotifiedAt = 0
	channelUpstreamModelUpdateNotifyState.lastChangedChannels = 0
	channelUpstreamModelUpdateNotifyState.lastFailedChannels = 0
	channelUpstreamModelUpdateNotifyState.Unlock()

	baseTime := int64(2000000)

	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime, 6, 0))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+3600, 6, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+3600, 7, 0))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+7200, 7, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+8000, 0, 3))
	require.False(t, shouldSendUpstreamModelUpdateNotification(baseTime+9000, 0, 3))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+10000, 0, 4))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+90000, 7, 0))
	require.True(t, shouldSendUpstreamModelUpdateNotification(baseTime+90001, 0, 0))
}
