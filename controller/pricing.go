package controller

import (
	"github.com/Jwell-ai/jwell-api/common"
	"github.com/Jwell-ai/jwell-api/model"
	"github.com/Jwell-ai/jwell-api/service"
	"github.com/Jwell-ai/jwell-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

// applyGroupRatioToPricing multiplies each model's ModelRatio and ModelPrice by
// the effective group ratio for that model. The group ratio is determined by the
// first of the model's enable_groups that appears in groupRatio. This bakes the
// multiplier into the returned pricing so the frontend shows effective prices
// without needing to know about group ratios separately.
func applyGroupRatioToPricing(pricing []model.Pricing, groupRatio map[string]float64) []model.Pricing {
	if len(groupRatio) == 0 {
		return pricing
	}
	result := make([]model.Pricing, len(pricing))
	for i, item := range pricing {
		gr := 1.0
		for _, g := range item.EnableGroup {
			if r, ok := groupRatio[g]; ok && r > 0 {
				gr = r
				break
			}
		}
		if gr == 1.0 {
			result[i] = item
			continue
		}
		p := item
		p.ModelRatio *= gr
		p.ModelPrice *= gr
		if p.CacheRatio != nil {
			v := *p.CacheRatio * gr
			p.CacheRatio = &v
		}
		if p.CreateCacheRatio != nil {
			v := *p.CreateCacheRatio * gr
			p.CreateCacheRatio = &v
		}
		if p.ImageRatio != nil {
			v := *p.ImageRatio * gr
			p.ImageRatio = &v
		}
		if p.AudioRatio != nil {
			v := *p.AudioRatio * gr
			p.AudioRatio = &v
		}
		result[i] = p
	}
	return result
}

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
	}
	var group string
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup = service.GetUserUsableGroups(group)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// Restrict groupRatio to usable groups only.
	for g := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[g]; !ok {
			delete(groupRatio, g)
		}
	}

	// Bake group ratio into model pricing so the frontend shows effective prices.
	pricing = applyGroupRatioToPricing(pricing, groupRatio)
	// Report all group ratios as 1 — they are already reflected in the prices above.
	flatGroupRatio := make(map[string]float64, len(groupRatio))
	for g := range groupRatio {
		flatGroupRatio[g] = 1.0
	}

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        flatGroupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        service.GetUserAutoGroup(group),
		"pricing_version":    "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
