package server

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
	"gorm.io/gorm"

	"app/internal/config"
	"app/internal/features/ai"
	"app/internal/features/auth"
	"app/internal/features/post"
	"app/internal/middleware"
	"app/internal/response"
)

// SpecFunc menghasilkan spesifikasi OpenAPI (JSON) untuk host dan skema pemanggil.
type SpecFunc func(host, scheme string) ([]byte, error)

// NewRouter merakit seluruh rute aplikasi.
func NewRouter(cfg config.Config, db *gorm.DB, specJSON SpecFunc) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.CustomRecovery(func(c *gin.Context, _ any) {
		response.AbortError(c, http.StatusInternalServerError, "Terjadi kesalahan pada server")
	}))
	_ = r.SetTrustedProxies(nil) // ClientIP memakai alamat koneksi asli
	r.Use(middleware.CORS(), middleware.NewRateLimiter(cfg.RateLimitGlobal, time.Minute))

	authService := auth.NewService(auth.NewRepository(db), cfg.AppKey, auth.TokenTTL)
	postService := post.NewService(post.NewRepository(db))
	aiService := ai.NewService(
		ai.NewSearchClient(cfg.SearchEngineEndpoint, cfg.SearchEngineKey),
		ai.NewOpenAIClient(cfg.OpenAIEndpoint, cfg.OpenAIKey, cfg.OpenAIModel),
	)

	authMiddleware := middleware.Auth(cfg.AppKey)
	public := r.Group("/api")
	authPublic := r.Group("/api", middleware.NewRateLimiter(cfg.RateLimitAuth, time.Minute))
	protected := r.Group("/api", authMiddleware)
	admin := r.Group("/api/admin", authMiddleware, middleware.RequireRole(auth.RoleAdmin))

	auth.NewHandler(authService).RegisterRoutes(authPublic, protected)
	post.NewHandler(postService).RegisterRoutes(public, protected, admin)
	ai.NewHandler(aiService).RegisterRoutes(protected, middleware.NewRateLimiter(cfg.RateLimitAI, time.Minute))

	r.GET("/api.json", specHandler(specJSON))
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler, ginSwagger.URL("/api.json")))

	r.NoRoute(func(c *gin.Context) {
		response.Fail(c, http.StatusNotFound, "Endpoint not found")
	})
	return r
}

// specHandler menyajikan spesifikasi OpenAPI yang disesuaikan dengan host pemanggil.
func specHandler(specJSON SpecFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme := "http"
		if c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}

		body, err := specJSON(c.Request.Host, scheme)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, "Gagal membuat spesifikasi API")
			return
		}

		c.Header("Cache-Control", "no-store") // isi bergantung pada header Host
		c.Data(http.StatusOK, "application/json; charset=utf-8", body)
	}
}
