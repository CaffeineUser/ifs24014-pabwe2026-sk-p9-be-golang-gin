package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoadDefaults(t *testing.T) {
	for _, k := range []string{
		"APP_ENV", "APP_PORT", "APP_KEY", "ADMIN_USERNAME", "ADMIN_PASSWORD",
		"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME",
		"DB_PASSWORD", "DB_SSLMODE", "SEARCH_ENGINE_ENDPOINT", "OPENAI_ENDPOINT",
		"OPENAI_KEY", "OPENAI_MODEL",
	} {
		t.Setenv(k, "")
	}

	cfg := Load()

	assert.Equal(t, "development", cfg.AppEnv)
	assert.Equal(t, "8081", cfg.AppPort)
	assert.Equal(t, "admin", cfg.AdminUsername)
	assert.Equal(t, "pabwe_p9_blog", cfg.DBDatabase)
	assert.Equal(t, "https://delcom.org/api/v1/search", cfg.SearchEngineEndpoint)
	assert.Equal(t, "", cfg.SearchEngineKey)
	assert.Equal(t, "", cfg.OpenAIKey)
}

func TestLoadFromEnvironment(t *testing.T) {
	t.Setenv("APP_PORT", "9999")
	t.Setenv("DB_HOST", "db.internal")
	t.Setenv("OPENAI_MODEL", "model-x")
	t.Setenv("SEARCH_ENGINE_KEY", "kunci-pencarian")

	cfg := Load()

	assert.Equal(t, "9999", cfg.AppPort)
	assert.Equal(t, "db.internal", cfg.DBHost)
	assert.Equal(t, "model-x", cfg.OpenAIModel)
	assert.Equal(t, "kunci-pencarian", cfg.SearchEngineKey)
}

func TestDSN(t *testing.T) {
	cfg := Config{DBHost: "h", DBUsername: "u", DBPassword: "p", DBDatabase: "d", DBPort: "1", DBSSLMode: "disable"}
	assert.Equal(t,
		"host=h user=u password=p dbname=d port=1 sslmode=disable TimeZone=Asia/Jakarta",
		cfg.DSN())
}
