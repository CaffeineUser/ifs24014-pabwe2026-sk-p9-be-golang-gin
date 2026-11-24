package ai

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"app/internal/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes: protected harus sudah memakai middleware.Auth; limiter membatasi frekuensi panggilan.
func (h *Handler) RegisterRoutes(protected *gin.RouterGroup, limiter gin.HandlerFunc) {
	protected.POST("/posts/ai-generate", limiter, h.Generate)
}

func writeError(c *gin.Context, err error) {
	var validationErr *ValidationError
	var upstreamErr *UpstreamError

	switch {
	case errors.As(err, &validationErr):
		response.Fail(c, http.StatusBadRequest, validationErr.Message)
	case errors.Is(err, ErrNotConfigured):
		response.Error(c, http.StatusServiceUnavailable, err.Error())
	case errors.As(err, &upstreamErr) && upstreamErr.StatusCode == http.StatusTooManyRequests:
		log.Printf("ai-generate: kuota layanan AI habis: %v", err)
		response.Fail(c, http.StatusTooManyRequests, "Kuota layanan AI sedang habis, coba lagi beberapa saat lagi")
	default: // detail penyedia AI hanya dicatat di log server, tidak diteruskan ke klien
		log.Printf("ai-generate gagal: %v", err)
		response.Error(c, http.StatusBadGateway, "Layanan AI gagal menghasilkan artikel, silakan coba lagi")
	}
}

// Generate godoc
// @Summary   Hasilkan draf artikel dengan AI (opsional + pencarian web)
// @Tags      posts
// @Accept    json
// @Produce   json
// @Security  BearerAuth
// @Param     payload body GenerateRequest true "Topik dan kategori"
// @Success   200 {object} response.Body
// @Failure   400 {object} response.Body
// @Failure   429 {object} response.Body
// @Failure   502 {object} response.Body
// @Failure   503 {object} response.Body
// @Router    /posts/ai-generate [post]
func (h *Handler) Generate(c *gin.Context) {
	var req GenerateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "Format data tidak valid")
		return
	}

	result, err := h.service.Generate(c.Request.Context(), req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Draf artikel berhasil dibuat", result)
}
