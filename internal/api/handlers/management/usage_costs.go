package management

import (
	"errors"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagestats"
)

const maxModelPriceUSDPerMillion = 1_000_000.0

type modelPricePayload struct {
	Model                   string   `json:"model"`
	InputPerMillionUSD      *float64 `json:"input_per_million_usd"`
	OutputPerMillionUSD     *float64 `json:"output_per_million_usd"`
	CacheReadPerMillionUSD  *float64 `json:"cache_read_per_million_usd"`
	CacheWritePerMillionUSD *float64 `json:"cache_write_per_million_usd"`
}

type modelPriceResponse struct {
	Model                   string  `json:"model"`
	InputPerMillionUSD      float64 `json:"input_per_million_usd"`
	OutputPerMillionUSD     float64 `json:"output_per_million_usd"`
	CacheReadPerMillionUSD  float64 `json:"cache_read_per_million_usd"`
	CacheWritePerMillionUSD float64 `json:"cache_write_per_million_usd"`
	CacheReadConfigured     bool    `json:"cache_read_configured"`
	CacheWriteConfigured    bool    `json:"cache_write_configured"`
	Source                  string  `json:"source"`
	UpdatedAtMS             int64   `json:"updated_at_ms"`
}

func (h *Handler) usageStore() *usagestats.Store {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	store := h.usageStatsStore
	h.mu.Unlock()
	return store
}

// GetUsageCosts returns all-time account and model cost estimates.
func (h *Handler) GetUsageCosts(c *gin.Context) {
	store := h.usageStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage_statistics_unavailable", "message": "usage statistics store unavailable"})
		return
	}
	c.JSON(http.StatusOK, store.Summary())
}

// GetModelPrices returns the local model price book and observed model names.
func (h *Handler) GetModelPrices(c *gin.Context) {
	store := h.usageStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage_statistics_unavailable", "message": "usage statistics store unavailable"})
		return
	}
	prices := store.ListPrices()
	response := make([]modelPriceResponse, 0, len(prices))
	for _, price := range prices {
		response = append(response, modelPriceToResponse(price))
	}
	summary := store.Summary()
	observedModels := make([]string, 0, len(summary.Models))
	for _, model := range summary.Models {
		observedModels = append(observedModels, model.Model)
	}
	sort.Strings(observedModels)
	c.JSON(http.StatusOK, gin.H{"prices": response, "observed_models": observedModels})
}

// PutModelPrice creates or replaces one local model price.
func (h *Handler) PutModelPrice(c *gin.Context) {
	store := h.usageStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage_statistics_unavailable", "message": "usage statistics store unavailable"})
		return
	}
	var payload modelPricePayload
	if errBind := c.ShouldBindJSON(&payload); errBind != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_model_price", "message": "invalid model price payload"})
		return
	}
	price, errPrice := modelPriceFromPayload(payload)
	if errPrice != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_model_price", "message": errPrice.Error()})
		return
	}
	if errSave := store.UpsertPrice(price); errSave != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "model_price_save_failed", "message": errSave.Error()})
		return
	}
	for _, savedPrice := range store.ListPrices() {
		if strings.EqualFold(savedPrice.Model, price.Model) {
			price = savedPrice
			break
		}
	}
	c.JSON(http.StatusOK, modelPriceToResponse(price))
}

// DeleteModelPrice deletes one local model price selected by the model query parameter.
func (h *Handler) DeleteModelPrice(c *gin.Context) {
	store := h.usageStore()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "usage_statistics_unavailable", "message": "usage statistics store unavailable"})
		return
	}
	model := strings.TrimSpace(c.Query("model"))
	if model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_required", "message": "model is required"})
		return
	}
	if errDelete := store.DeletePrice(model); errDelete != nil {
		if errors.Is(errDelete, os.ErrNotExist) {
			c.JSON(http.StatusNotFound, gin.H{"error": "model_price_not_found", "message": "model price not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "model_price_delete_failed", "message": errDelete.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func modelPriceFromPayload(payload modelPricePayload) (usagestats.ModelPrice, error) {
	model := strings.TrimSpace(payload.Model)
	if model == "" {
		return usagestats.ModelPrice{}, errors.New("model is required")
	}
	if payload.InputPerMillionUSD == nil || payload.OutputPerMillionUSD == nil {
		return usagestats.ModelPrice{}, errors.New("input and output prices are required")
	}
	input, errInput := usdToMicros(*payload.InputPerMillionUSD)
	if errInput != nil {
		return usagestats.ModelPrice{}, errors.New("input price must be a finite non-negative number")
	}
	output, errOutput := usdToMicros(*payload.OutputPerMillionUSD)
	if errOutput != nil {
		return usagestats.ModelPrice{}, errors.New("output price must be a finite non-negative number")
	}
	price := usagestats.ModelPrice{
		Model:                  model,
		InputMicrosPerMillion:  input,
		OutputMicrosPerMillion: output,
		Source:                 "manual",
	}
	if payload.CacheReadPerMillionUSD != nil {
		cacheRead, errCacheRead := usdToMicros(*payload.CacheReadPerMillionUSD)
		if errCacheRead != nil {
			return usagestats.ModelPrice{}, errors.New("cache read price must be a finite non-negative number")
		}
		price.CacheReadMicrosPerMillion = cacheRead
		price.CacheReadConfigured = true
	}
	if payload.CacheWritePerMillionUSD != nil {
		cacheWrite, errCacheWrite := usdToMicros(*payload.CacheWritePerMillionUSD)
		if errCacheWrite != nil {
			return usagestats.ModelPrice{}, errors.New("cache write price must be a finite non-negative number")
		}
		price.CacheWriteMicrosPerMillion = cacheWrite
		price.CacheWriteConfigured = true
	}
	return price, nil
}

func usdToMicros(value float64) (int64, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maxModelPriceUSDPerMillion {
		return 0, errors.New("invalid USD price")
	}
	return int64(math.Round(value * 1_000_000)), nil
}

func modelPriceToResponse(price usagestats.ModelPrice) modelPriceResponse {
	return modelPriceResponse{
		Model:                   price.Model,
		InputPerMillionUSD:      float64(price.InputMicrosPerMillion) / 1_000_000,
		OutputPerMillionUSD:     float64(price.OutputMicrosPerMillion) / 1_000_000,
		CacheReadPerMillionUSD:  float64(price.CacheReadMicrosPerMillion) / 1_000_000,
		CacheWritePerMillionUSD: float64(price.CacheWriteMicrosPerMillion) / 1_000_000,
		CacheReadConfigured:     price.CacheReadConfigured,
		CacheWriteConfigured:    price.CacheWriteConfigured,
		Source:                  price.Source,
		UpdatedAtMS:             price.UpdatedAtMS,
	}
}
