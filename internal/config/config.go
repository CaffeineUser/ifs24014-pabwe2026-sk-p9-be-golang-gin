package config

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv  string
	AppPort string
	AppKey  string

	AdminUsername string
	AdminPassword string

	DBConnection string
	DBHost       string
	DBPort       string
	DBDatabase   string
	DBUsername   string
	DBPassword   string
	DBSSLMode    string

	SearchEngineEndpoint string
	SearchEngineKey      string
	OpenAIEndpoint       string
	OpenAIKey            string
	OpenAIModel          string

	RateLimitGlobal int
	RateLimitAuth   int
	RateLimitAI     int
}

// getEnvInt membaca bilangan bulat positif; nilai kosong, rusak, atau <= 0 memakai fallback.
func getEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func Load() Config {
	_ = godotenv.Load()

	return Config{
		AppEnv:  getEnv("APP_ENV", "development"),
		AppPort: getEnv("APP_PORT", "8081"),
		AppKey:  getEnv("APP_KEY", "delcom_blog_secret_key_super_secure_2026_random_string_32chars"),

		AdminUsername: getEnv("ADMIN_USERNAME", "admin"),
		AdminPassword: getEnv("ADMIN_PASSWORD", "admin"),

		DBConnection: getEnv("DB_CONNECTION", "pgsql"),
		DBHost:       getEnv("DB_HOST", "127.0.0.1"),
		DBPort:       getEnv("DB_PORT", "5432"),
		DBDatabase:   getEnv("DB_DATABASE", "pabwe_p9_blog"),
		DBUsername:   getEnv("DB_USERNAME", "postgres"),
		DBPassword:   getEnv("DB_PASSWORD", "postgres"),
		DBSSLMode:    getEnv("DB_SSL_MODE", "disable"),

		SearchEngineEndpoint: getEnv("SEARCH_ENGINE_ENDPOINT", "https://delcom.org/api/v1/search"),
		SearchEngineKey:      getEnv("SEARCH_ENGINE_KEY", ""),
		OpenAIEndpoint:       getEnv("OPENAI_ENDPOINT", getEnv("OPENAI_BASE_URL", "https://generativelanguage.googleapis.com/v1beta/openai")),
		OpenAIKey:            getEnv("OPENAI_KEY", getEnv("OPENAI_API_KEY", getEnv("GEMINI_API_KEY", ""))),
		OpenAIModel:          getEnv("OPENAI_MODEL", "gemini-3.5-flash-lite"),
		RateLimitGlobal:      getEnvInt("RATE_LIMIT_GLOBAL", 10000),
		RateLimitAuth:        getEnvInt("RATE_LIMIT_AUTH", 10000),
		RateLimitAI:          getEnvInt("RATE_LIMIT_AI", 1000),
	}
}

func (c Config) DSN() string {
	return fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=Asia/Jakarta",
		c.DBHost, c.DBUsername, c.DBPassword, c.DBDatabase, c.DBPort, c.DBSSLMode,
	)
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
