package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"app/internal/middleware"
	"app/internal/response"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// RegisterRoutes memasang endpoint /auth/*. `protected` harus sudah memakai middleware.Auth.
func (h *Handler) RegisterRoutes(public, protected *gin.RouterGroup) {
	public.POST("/auth/register", h.Register)
	public.POST("/auth/login", h.Login)
	protected.POST("/auth/logout", h.Logout)
	protected.GET("/auth/profile", h.GetProfile)
	protected.PUT("/auth/profile", h.UpdateProfile)
}

// writeError memetakan error service ke respons HTTP yang tepat.
func writeError(c *gin.Context, err error) {
	var validationErr *ValidationError
	switch {
	case errors.As(err, &validationErr):
		response.Fail(c, http.StatusBadRequest, validationErr.Message)
	case errors.Is(err, ErrUsernameTaken), errors.Is(err, ErrEmailTaken):
		response.Fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrUserNotFound):
		response.Fail(c, http.StatusUnauthorized, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, "Terjadi kesalahan pada server")
	}
}

// Register godoc
// @Summary     Registrasi akun penulis baru
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       payload body RegisterRequest true "Data registrasi"
// @Success     201 {object} response.Body
// @Failure     400 {object} response.Body
// @Failure     409 {object} response.Body
// @Router      /auth/register [post]
func (h *Handler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "Format data tidak valid")
		return
	}

	res, err := h.service.Register(req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "Registrasi berhasil", res)
}

// Login godoc
// @Summary     Login dan terima token JWT
// @Tags        auth
// @Accept      json
// @Produce     json
// @Param       payload body LoginRequest true "Kredensial"
// @Success     200 {object} response.Body
// @Failure     400 {object} response.Body
// @Failure     401 {object} response.Body
// @Router      /auth/login [post]
func (h *Handler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "Format data tidak valid")
		return
	}

	result, err := h.service.Login(req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Login berhasil", result)
}

// Logout godoc
// @Summary     Logout (token dibuang di sisi klien)
// @Tags        auth
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} response.Body
// @Failure     401 {object} response.Body
// @Router      /auth/logout [post]
func (h *Handler) Logout(c *gin.Context) {
	response.Success(c, http.StatusOK, "Logout berhasil", nil)
}

// GetProfile godoc
// @Summary     Ambil profil pengguna yang sedang login
// @Tags        auth
// @Produce     json
// @Security    BearerAuth
// @Success     200 {object} response.Body
// @Failure     401 {object} response.Body
// @Router      /auth/profile [get]
func (h *Handler) GetProfile(c *gin.Context) {
	current, _ := middleware.CurrentUser(c)

	user, err := h.service.GetProfile(current.ID)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Profil berhasil diambil", ToUserResponse(*user))
}

// UpdateProfile godoc
// @Summary     Perbarui profil pengguna yang sedang login
// @Tags        auth
// @Accept      json
// @Produce     json
// @Security    BearerAuth
// @Param       payload body UpdateProfileRequest true "Data profil"
// @Success     200 {object} response.Body
// @Failure     400 {object} response.Body
// @Failure     401 {object} response.Body
// @Failure     409 {object} response.Body
// @Router      /auth/profile [put]
func (h *Handler) UpdateProfile(c *gin.Context) {
	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, "Format data tidak valid")
		return
	}

	current, _ := middleware.CurrentUser(c)
	user, err := h.service.UpdateProfile(current.ID, req)
	if err != nil {
		writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "Profil berhasil diperbarui", ToUserResponse(*user))
}
