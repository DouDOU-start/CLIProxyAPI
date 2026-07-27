package management

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/usagestats"
	coreusage "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/usage"
)

func newUsageCostsTestRouter(t *testing.T) (*gin.Engine, *usagestats.Store) {
	t.Helper()
	store, errStore := usagestats.NewStore(filepath.Join(t.TempDir(), "usage-statistics.json"), true)
	if errStore != nil {
		t.Fatalf("NewStore() error = %v", errStore)
	}
	t.Cleanup(store.Close)
	handler := &Handler{usageStatsStore: store}
	router := gin.New()
	router.GET("/usage-costs", handler.GetUsageCosts)
	router.GET("/model-prices", handler.GetModelPrices)
	router.PUT("/model-prices", handler.PutModelPrice)
	router.DELETE("/model-prices", handler.DeleteModelPrice)
	return router, store
}

func TestUsageCostsManagementFlow(t *testing.T) {
	router, store := newUsageCostsTestRouter(t)

	priceBody := []byte(`{"model":"gpt-test","input_per_million_usd":2,"output_per_million_usd":4}`)
	priceRecorder := httptest.NewRecorder()
	priceRequest := httptest.NewRequest(http.MethodPut, "/model-prices", bytes.NewReader(priceBody))
	priceRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(priceRecorder, priceRequest)
	if priceRecorder.Code != http.StatusOK {
		t.Fatalf("put price status = %d, want %d body=%s", priceRecorder.Code, http.StatusOK, priceRecorder.Body.String())
	}
	var price modelPriceResponse
	if errDecode := json.Unmarshal(priceRecorder.Body.Bytes(), &price); errDecode != nil {
		t.Fatalf("decode price response: %v", errDecode)
	}
	if price.Model != "gpt-test" || price.InputPerMillionUSD != 2 || price.OutputPerMillionUSD != 4 {
		t.Fatalf("unexpected price response: %#v", price)
	}
	if price.CacheReadConfigured || price.CacheWriteConfigured {
		t.Fatalf("optional cache prices should be automatic: %#v", price)
	}

	store.HandleUsage(context.Background(), coreusage.Record{
		Provider:  "openai",
		Model:     "gpt-test",
		AuthIndex: "auth-index-1",
		AuthType:  "apikey",
		Source:    "secret-upstream-api-key",
		Detail: coreusage.Detail{
			InputTokens:  1_000_000,
			OutputTokens: 500_000,
			TotalTokens:  1_500_000,
		},
	})

	usageRecorder := httptest.NewRecorder()
	router.ServeHTTP(usageRecorder, httptest.NewRequest(http.MethodGet, "/usage-costs", nil))
	if usageRecorder.Code != http.StatusOK {
		t.Fatalf("get usage status = %d, want %d body=%s", usageRecorder.Code, http.StatusOK, usageRecorder.Body.String())
	}
	var summary usagestats.Summary
	if errDecode := json.Unmarshal(usageRecorder.Body.Bytes(), &summary); errDecode != nil {
		t.Fatalf("decode usage response: %v", errDecode)
	}
	if summary.Calls != 1 || summary.TotalTokens != 1_500_000 || summary.CostMicros != 4_000_000 {
		t.Fatalf("unexpected usage summary: %#v", summary)
	}

}

func TestPutModelPriceRejectsNegativePrice(t *testing.T) {
	router, _ := newUsageCostsTestRouter(t)
	body := []byte(`{"model":"gpt-test","input_per_million_usd":-1,"output_per_million_usd":4}`)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPut, "/model-prices", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
}
