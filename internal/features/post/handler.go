package post

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"app/internal/features/auth"
	"app/internal/middleware"
	"app/internal/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes: public = tanpa login, protected = sudah memakai middleware.Auth,
// admin = sudah memakai middleware.Auth + RequireRole(admin).
func (h *Handler) RegisterRoutes(public, protected, admin *gin.RouterGroup) {
	public.GET("/public/categories", h.Categories)
	public.GET("/public/posts", h.PublicList)
	public.GET("/public/posts/:slug", h.PublicDetail)

	protected.GET("/posts", h.ListMine)
	protected.POST("/posts", h.Create)
	protected.GET("/posts/stats", h.Stats)
	protected.GET("/posts/:id", h.GetOwned)
	protected.PUT("/posts/:id", h.Update)
	protected.PATCH("/posts/:id/toggle-publish", h.TogglePublish)
	protected.DELETE("/posts/:id", h.Delete)

	admin.GET("/users", h.AdminUsers)
	admin.GET("/posts", h.AdminPosts)
	admin.GET("/users/:id/posts", h.AdminUserPosts)
}

func writeError(c *gin.Context, err error) {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr):
		response.Fail(c, http.StatusBadRequest, validationErr.Message)
	case errors.Is(err, ErrForbidden):
		response.Fail(c, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrPostNotFound), errors.Is(err, auth.ErrUserNotFound):
		response.Fail(c, http.StatusNotFound, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "Terjadi kesalahan pada server")
	}
}

// parseID memvalidasi parameter ID. Jika bukan angka positif, kembalikan 400.
func parseID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 32)
	if err != nil || id == 0 {
		response.Fail(c, http.StatusBadRequest, "ID harus berupa angka positif")
		return 0, false
	}
	return uint(id), true
}

func currentUserID(c *gin.Context) uint {
	user, _ := middleware.CurrentUser(c)
	return user.ID
}

// Categories godoc
// @Summary  Daftar kategori artikel
// @Tags     public
// @Produce  json
// @Success  200 {object} response.Body
// @Router   /public/categories [get]
func (h *Handler) Categories(c *gin.Context) {
	response.Success(c, http.StatusOK, "Daftar kategori berhasil diambil", Categories)
}

// PublicList godoc
// @Summary  Daftar artikel terbit (pencarian & filter kategori)
// @Tags     public
// @Produce  json
// @Param    q        query string false "Kata kunci"
// @Param    category query string false "Kategori"
// @Success  200 {object} response.Body
// @Router   /public/posts [get]
func (h *Handler) PublicList(c *gin.Context) {
	posts, err := h.service.ListPublished(c.Query("q"), c.Query("category"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Daftar artikel berhasil diambil", posts)
}

// PublicDetail godoc
// @Summary  Detail artikel terbit berdasarkan slug
// @Tags     public
// @Produce  json
// @Param    slug path string true "Slug artikel"
// @Success  200 {object} response.Body
// @Failure  404 {object} response.Body
// @Router   /public/posts/{slug} [get]
func (h *Handler) PublicDetail(c *gin.Context) {
	post, err := h.service.GetPublished(c.Param("slug"))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Artikel berhasil diambil", post)
}

// ListMine godoc
// @Summary   Daftar artikel milik penulis (terbit & draf)
// @Tags      posts
// @Produce   json
// @Security  BearerAuth
// @Success   200 {object} response.Body
// @Router    /posts [get]
func (h *Handler) ListMine(c *gin.Context) {
	posts, err := h.service.ListMine(currentUserID(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Daftar artikel berhasil diambil", posts)
}

// Create godoc
// @Summary   Buat artikel baru (draf, slug otomatis)
// @Tags      posts
// @Accept    json
// @Produce   json
// @Security  BearerAuth
// @Param     payload body PostRequest true "Data artikel"
// @Success   201 {object} response.Body
// @Failure   400 {object} response.Body
// @Router    /posts [post]
func (h *Handler) Create(c *gin.Context) {
	var req PostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "Format data tidak valid")
		return
	}

	post, err := h.service.Create(currentUserID(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "Artikel berhasil dibuat", post)
}

// Stats godoc
// @Summary   Ringkasan statistik artikel penulis
// @Tags      posts
// @Produce   json
// @Security  BearerAuth
// @Success   200 {object} response.Body
// @Router    /posts/stats [get]
func (h *Handler) Stats(c *gin.Context) {
	stats, err := h.service.Stats(currentUserID(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Statistik berhasil diambil", stats)
}

// GetOwned godoc
// @Summary   Detail artikel milik penulis
// @Tags      posts
// @Produce   json
// @Security  BearerAuth
// @Param     id path int true "ID artikel"
// @Success   200 {object} response.Body
// @Failure   404 {object} response.Body
// @Router    /posts/{id} [get]
func (h *Handler) GetOwned(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	post, err := h.service.GetOwned(id, currentUserID(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Artikel berhasil diambil", post)
}

// Update godoc
// @Summary   Perbarui artikel milik penulis
// @Tags      posts
// @Accept    json
// @Produce   json
// @Security  BearerAuth
// @Param     id      path int         true "ID artikel"
// @Param     payload body PostRequest true "Data artikel"
// @Success   200 {object} response.Body
// @Failure   400 {object} response.Body
// @Failure   404 {object} response.Body
// @Router    /posts/{id} [put]
func (h *Handler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req PostRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "Format data tidak valid")
		return
	}

	post, err := h.service.Update(id, currentUserID(c), req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Artikel berhasil diperbarui", post)
}

// TogglePublish godoc
// @Summary   Ubah status Draf <-> Terbit
// @Tags      posts
// @Produce   json
// @Security  BearerAuth
// @Param     id path int true "ID artikel"
// @Success   200 {object} response.Body
// @Failure   400 {object} response.Body
// @Failure   404 {object} response.Body
// @Router    /posts/{id}/toggle-publish [patch]
func (h *Handler) TogglePublish(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	post, err := h.service.TogglePublish(id, currentUserID(c))
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Status publikasi berhasil diubah", post)
}

// Delete godoc
// @Summary   Hapus artikel milik penulis
// @Tags      posts
// @Produce   json
// @Security  BearerAuth
// @Param     id path int true "ID artikel"
// @Success   200 {object} response.Body
// @Failure   404 {object} response.Body
// @Router    /posts/{id} [delete]
func (h *Handler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(id, currentUserID(c)); err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Artikel berhasil dihapus", nil)
}

// AdminUsers godoc
// @Summary   [Admin] Daftar pengguna beserta total artikel
// @Tags      admin
// @Produce   json
// @Security  BearerAuth
// @Success   200 {object} response.Body
// @Failure   403 {object} response.Body
// @Router    /admin/users [get]
func (h *Handler) AdminUsers(c *gin.Context) {
	users, err := h.service.ListUsers()
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Daftar pengguna berhasil diambil", users)
}

// AdminPosts godoc
// @Summary   [Admin] Seluruh artikel dari semua penulis
// @Tags      admin
// @Produce   json
// @Security  BearerAuth
// @Success   200 {object} response.Body
// @Failure   403 {object} response.Body
// @Router    /admin/posts [get]
func (h *Handler) AdminPosts(c *gin.Context) {
	posts, err := h.service.ListAllPosts()
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Daftar artikel berhasil diambil", posts)
}

// AdminUserPosts godoc
// @Summary   [Admin] Artikel milik pengguna tertentu
// @Tags      admin
// @Produce   json
// @Security  BearerAuth
// @Param     id path int true "ID pengguna"
// @Success   200 {object} response.Body
// @Failure   404 {object} response.Body
// @Router    /admin/users/{id}/posts [get]
func (h *Handler) AdminUserPosts(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	posts, err := h.service.ListPostsByUser(id)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Daftar artikel pengguna berhasil diambil", posts)
}
