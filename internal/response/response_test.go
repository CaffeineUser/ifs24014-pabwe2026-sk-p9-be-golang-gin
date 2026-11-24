package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(handlers ...gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", handlers...)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

func TestSuccessWithData(t *testing.T) {
	w := serve(func(c *gin.Context) {
		Success(c, http.StatusCreated, "dibuat", gin.H{"id": 1})
	})

	assert.Equal(t, http.StatusCreated, w.Code)
	body := decode(t, w)
	assert.Equal(t, "success", body["status"])
	assert.Equal(t, "dibuat", body["message"])
	assert.Equal(t, map[string]any{"id": float64(1)}, body["data"])
}

func TestSuccessWithoutDataOmitsField(t *testing.T) {
	w := serve(func(c *gin.Context) { Success(c, http.StatusOK, "ok", nil) })

	body := decode(t, w)
	assert.Equal(t, "success", body["status"])
	assert.NotContains(t, body, "data")

}

func TestSuccessEmptySliceIsKept(t *testing.T) {
	w := serve(func(c *gin.Context) { Success(c, http.StatusOK, "ok", []string{}) })

	body := decode(t, w)
	assert.Equal(t, []any{}, body["data"])
}

func TestFail(t *testing.T) {
	w := serve(func(c *gin.Context) { Fail(c, http.StatusNotFound, "tidak ada") })

	assert.Equal(t, http.StatusNotFound, w.Code)
	body := decode(t, w)
	assert.Equal(t, "fail", body["status"])
	assert.Equal(t, "tidak ada", body["message"])
	assert.NotContains(t, body, "data")
}

func TestError(t *testing.T) {
	w := serve(func(c *gin.Context) { Error(c, http.StatusInternalServerError, "rusak") })

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "error", decode(t, w)["status"])
}

func TestAbortFailStopsChain(t *testing.T) {
	called := false
	w := serve(
		func(c *gin.Context) { AbortFail(c, http.StatusUnauthorized, "ditolak") },
		func(c *gin.Context) { called = true },
	)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "fail", decode(t, w)["status"])
	assert.False(t, called)
}

func TestAbortErrorStopsChain(t *testing.T) {
	called := false
	w := serve(
		func(c *gin.Context) { AbortError(c, http.StatusServiceUnavailable, "down") },
		func(c *gin.Context) { called = true },
	)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Equal(t, "error", decode(t, w)["status"])
	assert.False(t, called)
}
