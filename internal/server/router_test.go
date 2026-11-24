package server

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app/internal/config"
	"app/internal/database"
)

const testKey = "kunci-uji-minimal-32-karakter-aman-sekali!!"

// ---------- Helper ----------

func fakeSpec(host, scheme string) ([]byte, error) {
	return fmt.Appendf(nil, `{"host":%q,"scheme":%q}`, host, scheme), nil
}

func newRouterWithSpec(t *testing.T, cfg config.Config, spec SpecFunc) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	cfg.AppKey = testKey
	cfg.AdminUsername = "admin"
	cfg.AdminPassword = "admin"

	db, err := database.NewSQLiteMemory()
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	require.NoError(t, database.Seed(db, cfg))
	return NewRouter(cfg, db, spec)
}

func newTestRouter(t *testing.T, cfg config.Config) *gin.Engine {
	t.Helper()
	return newRouterWithSpec(t, cfg, fakeSpec)
}

func serve(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func call(r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return serve(r, req)
}

func bodyOf(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func dataOf(t *testing.T, w *httptest.ResponseRecorder) any {
	t.Helper()
	return bodyOf(t, w)["data"]
}

func mustRegister(t *testing.T, r http.Handler, username string) {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"email":"%s@x.id","password":"rahasia123"}`, username, username)
	require.Equal(t, http.StatusCreated, call(r, http.MethodPost, "/api/auth/register", body, "").Code)
}

func mustLogin(t *testing.T, r http.Handler, username, password string) string {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, username, password)
	w := call(r, http.MethodPost, "/api/auth/login", body, "")
	require.Equal(t, http.StatusOK, w.Code)
	return dataOf(t, w).(map[string]any)["token"].(string)
}

// ---------- Infrastruktur ----------

func TestNoRouteReturnsStandardJSON(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/tidak-ada"},
		{http.MethodGet, "/"},
		{http.MethodPost, "/api/auth/tidak-ada"},
		{http.MethodDelete, "/api/public/posts"}, // metode salah pada rute yang ada
	} {
		w := call(r, tc.method, tc.path, "", "")
		assert.Equal(t, http.StatusNotFound, w.Code, tc.path)
		assert.JSONEq(t, `{"status":"fail","message":"Endpoint not found"}`, w.Body.String())
	}
}

func TestCORS(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	preflight := call(r, http.MethodOptions, "/api/posts", "", "")
	assert.Equal(t, http.StatusNoContent, preflight.Code)
	assert.Equal(t, "*", preflight.Header().Get("Access-Control-Allow-Origin"))

	normal := call(r, http.MethodGet, "/api/public/categories", "", "")
	assert.Equal(t, "*", normal.Header().Get("Access-Control-Allow-Origin"))
}

func TestPanicReturnsStandardJSON(t *testing.T) {
	r := newTestRouter(t, config.Config{})
	r.GET("/boom", func(*gin.Context) { panic("kaboom") })

	w := call(r, http.MethodGet, "/boom", "", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "error", bodyOf(t, w)["status"])
	assert.NotContains(t, w.Body.String(), "kaboom")
}

func TestSwaggerUI(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	w := call(r, http.MethodGet, "/swagger/index.html", "", "")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	assert.Contains(t, w.Body.String(), "swagger-ui")
}

func TestSpecEndpointAdaptsToCaller(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	t.Run("http bawaan", func(t *testing.T) {
		w := call(r, http.MethodGet, "/api.json", "", "")
		require.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
		assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		assert.JSONEq(t, `{"host":"example.com","scheme":"http"}`, w.Body.String())
	})

	t.Run("https lewat X-Forwarded-Proto", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api.json", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		assert.JSONEq(t, `{"host":"example.com","scheme":"https"}`, serve(r, req).Body.String())
	})

	t.Run("https lewat TLS langsung", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api.json", nil)
		req.TLS = &tls.ConnectionState{}
		assert.JSONEq(t, `{"host":"example.com","scheme":"https"}`, serve(r, req).Body.String())
	})
}

func TestSpecEndpointError(t *testing.T) {
	failing := func(string, string) ([]byte, error) { return nil, errors.New("boom") }
	r := newRouterWithSpec(t, config.Config{}, failing)

	w := call(r, http.MethodGet, "/api.json", "", "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "error", bodyOf(t, w)["status"])
	assert.NotContains(t, w.Body.String(), "boom")
}

// ---------- Keamanan rute ----------

func TestProtectedRoutesRequireToken(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/auth/logout"},
		{http.MethodGet, "/api/auth/profile"},
		{http.MethodPut, "/api/auth/profile"},
		{http.MethodGet, "/api/posts"},
		{http.MethodPost, "/api/posts"},
		{http.MethodGet, "/api/posts/stats"},
		{http.MethodGet, "/api/posts/1"},
		{http.MethodPut, "/api/posts/1"},
		{http.MethodPatch, "/api/posts/1/toggle-publish"},
		{http.MethodDelete, "/api/posts/1"},
		{http.MethodPost, "/api/posts/ai-generate"},
		{http.MethodGet, "/api/admin/users"},
		{http.MethodGet, "/api/admin/posts"},
		{http.MethodGet, "/api/admin/users/1/posts"},
	} {
		w := call(r, tc.method, tc.path, "{}", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, tc.method+" "+tc.path)
	}
}

func TestAuthRoutesAreRateLimited(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	for i := 0; i < authRatePerMinute; i++ {
		assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/auth/login", `{}`, "").Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, call(r, http.MethodPost, "/api/auth/login", `{}`, "").Code)
}

func TestAIRouteIsRateLimitedAfterAuth(t *testing.T) {
	r := newTestRouter(t, config.Config{})
	mustRegister(t, r, "budi")
	token := mustLogin(t, r, "budi", "rahasia123")

	for i := 0; i < aiRatePerMinute; i++ {
		assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/posts/ai-generate", `{"topic":""}`, token).Code)
	}
	assert.Equal(t, http.StatusTooManyRequests, call(r, http.MethodPost, "/api/posts/ai-generate", `{"topic":""}`, token).Code)
}

// ---------- Alur menyeluruh ----------

func TestEndToEndAuthorAndAdminFlow(t *testing.T) {
	r := newTestRouter(t, config.Config{})

	mustRegister(t, r, "budi")
	token := mustLogin(t, r, "budi", "rahasia123")

	w := call(r, http.MethodGet, "/api/auth/profile", "", token)
	require.Equal(t, http.StatusOK, w.Code)
	profile := dataOf(t, w).(map[string]any)
	assert.Equal(t, "budi", profile["username"])
	budiID := int(profile["id"].(float64))

	// Buat artikel (draf) -> belum tampil di publik.
	w = call(r, http.MethodPost, "/api/posts",
		`{"title":"Belajar Gin","category":"Teknologi","excerpt":"ringkas","content":"isi artikel lengkap"}`, token)
	require.Equal(t, http.StatusCreated, w.Code)
	postPath := fmt.Sprintf("/api/posts/%d", int(dataOf(t, w).(map[string]any)["id"].(float64)))
	assert.Equal(t, []any{}, dataOf(t, call(r, http.MethodGet, "/api/public/posts", "", "")))

	// Terbitkan -> tampil di publik (daftar & detail).
	w = call(r, http.MethodPatch, postPath+"/toggle-publish", "", token)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, true, dataOf(t, w).(map[string]any)["is_published"])

	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/public/posts?q=gin", "", "")), 1)
	w = call(r, http.MethodGet, "/api/public/posts/belajar-gin", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	detail := dataOf(t, w).(map[string]any)
	assert.Equal(t, "budi", detail["author"].(map[string]any)["username"])
	assert.Equal(t, "isi artikel lengkap", detail["content"])

	// Statistik penulis.
	stats := dataOf(t, call(r, http.MethodGet, "/api/posts/stats", "", token)).(map[string]any)
	assert.EqualValues(t, 1, stats["total_posts"])
	assert.EqualValues(t, 1, stats["total_published"])
	assert.EqualValues(t, 0, stats["total_draft"])

	// Member tidak boleh masuk area admin; admin boleh.
	assert.Equal(t, http.StatusForbidden, call(r, http.MethodGet, "/api/admin/users", "", token).Code)

	adminToken := mustLogin(t, r, "admin", "admin")
	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/admin/users", "", adminToken)), 3) // admin, member, budi
	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/admin/posts", "", adminToken)), 1)
	assert.Len(t, dataOf(t, call(r, http.MethodGet, fmt.Sprintf("/api/admin/users/%d/posts", budiID), "", adminToken)), 1)

	// Logout, lalu hapus artikel.
	assert.Equal(t, http.StatusOK, call(r, http.MethodPost, "/api/auth/logout", "", token).Code)
	assert.Equal(t, http.StatusOK, call(r, http.MethodDelete, postPath, "", token).Code)
	stats = dataOf(t, call(r, http.MethodGet, "/api/posts/stats", "", token)).(map[string]any)
	assert.EqualValues(t, 0, stats["total_posts"])
}

func TestEndToEndAIGenerateThenSave(t *testing.T) {
	article := `{"title":"Judul AI","category":"Teknologi","excerpt":"Ringkasan AI.","content":"Isi artikel dari AI."}`
	completion, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]string{"content": article}}},
	})
	require.NoError(t, err)

	openAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(completion)
	}))
	defer openAI.Close()

	search := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[{"title":"Sumber","url":"https://example.com/a","snippet":"ringkas"}]}`))
	}))
	defer search.Close()

	r := newTestRouter(t, config.Config{
		OpenAIEndpoint: openAI.URL, OpenAIKey: "k", OpenAIModel: "m",
		SearchEngineEndpoint: search.URL,
	})
	mustRegister(t, r, "budi")
	token := mustLogin(t, r, "budi", "rahasia123")

	w := call(r, http.MethodPost, "/api/posts/ai-generate", `{"topic":"Golang 1.27","category":"Teknologi"}`, token)
	require.Equal(t, http.StatusOK, w.Code)
	draft := dataOf(t, w).(map[string]any)
	assert.Equal(t, "Judul AI", draft["title"])
	assert.Len(t, draft["search_sources"], 1)
	assert.Contains(t, draft["content"], "## Referensi")

	// Draf AI disimpan lewat endpoint artikel biasa.
	saved, err := json.Marshal(map[string]any{
		"title": draft["title"], "category": draft["category"],
		"excerpt": draft["excerpt"], "content": draft["content"],
	})
	require.NoError(t, err)
	w = call(r, http.MethodPost, "/api/posts", string(saved), token)
	require.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "judul-ai", dataOf(t, w).(map[string]any)["slug"])
}

func TestAIGenerateNotConfigured(t *testing.T) {
	r := newTestRouter(t, config.Config{}) // tanpa OPENAI_*
	mustRegister(t, r, "budi")
	token := mustLogin(t, r, "budi", "rahasia123")

	w := call(r, http.MethodPost, "/api/posts/ai-generate", `{"topic":"Golang 1.27"}`, token)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}
