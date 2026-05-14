package controller

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

const exchangeRateAPIURL = "https://open.er-api.com/v6/latest/USD"

type erAPIResponse struct {
	Result string             `json:"result"`
	Rates  map[string]float64 `json:"rates"`
}

func FetchMarketExchangeRate(currency string) (float64, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(exchangeRateAPIURL)
	if err != nil {
		return 0, fmt.Errorf("fetch exchange rate failed: %w", err)
	}
	defer resp.Body.Close()
	var payload erAPIResponse
	if err := common.DecodeJson(resp.Body, &payload); err != nil {
		return 0, fmt.Errorf("decode exchange rate response failed: %w", err)
	}
	if payload.Result != "success" {
		return 0, fmt.Errorf("exchange rate API returned non-success result")
	}
	rate, ok := payload.Rates[currency]
	if !ok || rate <= 0 {
		return 0, fmt.Errorf("currency %q not found in exchange rate response", currency)
	}
	return rate, nil
}

func SyncExchangeRate(c *gin.Context) {
	currency := c.DefaultQuery("currency", "CNY")
	rate, err := FetchMarketExchangeRate(currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	rateStr := strconv.FormatFloat(rate, 'f', 4, 64)
	if err := model.UpdateOption("USDExchangeRate", rateStr); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	operation_setting.USDExchangeRate = rate
	common.SysLog(fmt.Sprintf("exchange rate synced from market: 1 USD = %.4f %s", rate, currency))
	c.JSON(http.StatusOK, gin.H{"success": true, "rate": rate, "currency": currency})
}
