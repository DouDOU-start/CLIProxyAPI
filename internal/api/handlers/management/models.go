package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/registry"
)

// GetModels returns the models currently available in the runtime registry.
func (h *Handler) GetModels(c *gin.Context) {
	models := registry.GetGlobalRegistry().GetAvailableModels("openai")
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   models,
	})
}
