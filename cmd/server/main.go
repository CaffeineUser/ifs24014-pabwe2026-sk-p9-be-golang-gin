package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"app/docs"
	"app/internal/config"
	"app/internal/database"
	"app/internal/server"
)

// @title           Delcom Blog API
// @version         1.0
// @description     REST API Delcom Blog: artikel publik, penulis, admin, dan generator artikel AI.
// @host            localhost:8081
// @BasePath        /api
// @securityDefinitions.apikey BearerAuth
// @in              header
// @name            Authorization
// @description     Isi dengan: Bearer {token JWT} (kata Bearer, spasi, lalu token dari /auth/login)
func main() {
	cfg := config.Load()
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("gagal terhubung ke database: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("gagal migrasi: %v", err)
	}
	if err := database.Seed(db, cfg); err != nil {
		log.Fatalf("gagal seeding: %v", err)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           server.NewRouter(cfg, db, docs.GetSwaggerJSON),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout sengaja tidak diatur: pembuatan artikel AI bisa memakan waktu lama.
	}

	log.Printf("[GIN-debug] Listening and serving HTTP on %s", srv.Addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server berhenti: %v", err)
	}
}
