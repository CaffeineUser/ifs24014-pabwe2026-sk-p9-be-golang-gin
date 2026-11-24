package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "test-secret-key-minimal-32-characters!!"

func newEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func request(method, path, authHeader, ip string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	if ip != "" {
		req.RemoteAddr = ip + ":1234"
	}
	return req
}

func serve(r *gin.Engine, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func signToken(t *testing.T, secret string, method jwt.SigningMethod, exp time.Time) string {
	t.Helper()
	claims := Claims{
		UserID: 7, Username: "budi", Role: "member",
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(exp)},
	}
	s, err := jwt.NewWithClaims(method, claims).SignedString([]byte(secret))
	require.NoError(t, err)
	return s
}

// ---------- CORS ----------

func TestCORSAddsHeaders(t *testing.T) {
	r := newEngine()
	r.Use(CORS())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	w := serve(r, request(http.MethodGet, "/ping", "", ""))

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "Authorization")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "PATCH")
}

func TestCORSAnswersPreflightEvenForUnknownRoute(t *testing.T) {
	r := newEngine()
	r.Use(CORS())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	w := serve(r, request(http.MethodOptions, "/api/posts", "", ""))

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
}

// ---------- Auth & CurrentUser ----------

func TestAuth(t *testing.T) {
	r := newEngine()
	r.GET("/me", Auth(testSecret), func(c *gin.Context) {
		user, ok := CurrentUser(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, user)
	})

	future := time.Now().Add(time.Hour)
	valid := signToken(t, testSecret, jwt.SigningMethodHS256, future)

	cases := []struct {
		name   string
		header string
		want   int
	}{
		{"tanpa header", "", http.StatusUnauthorized},
		{"skema bukan Bearer", "Token " + valid, http.StatusUnauthorized},
		{"Bearer tanpa token", "Bearer ", http.StatusUnauthorized},
		{"Bearer hanya spasi", "Bearer    ", http.StatusUnauthorized},
		{"token sampah", "Bearer abc.def.ghi", http.StatusUnauthorized},
		{"secret salah", "Bearer " + signToken(t, "secret-lain-yang-berbeda-1234567890", jwt.SigningMethodHS256, future), http.StatusUnauthorized},
		{"kedaluwarsa", "Bearer " + signToken(t, testSecret, jwt.SigningMethodHS256, time.Now().Add(-time.Hour)), http.StatusUnauthorized},
		{"algoritma HS512 ditolak", "Bearer " + signToken(t, testSecret, jwt.SigningMethodHS512, future), http.StatusUnauthorized},
		{"token valid", "Bearer " + valid, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(r, request(http.MethodGet, "/me", tc.header, ""))
			assert.Equal(t, tc.want, w.Code)
			if tc.want == http.StatusOK {
				assert.JSONEq(t, `{"id":7,"username":"budi","role":"member"}`, w.Body.String())
			} else {
				assert.Contains(t, w.Body.String(), `"status":"fail"`)
			}
		})
	}
}

func TestCurrentUserWithoutOrWrongType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())

	_, ok := CurrentUser(c)
	assert.False(t, ok)

	c.Set(UserContextKey, "bukan AuthUser")
	_, ok = CurrentUser(c)
	assert.False(t, ok)
}

// ---------- RequireRole ----------

func TestRequireRole(t *testing.T) {
	r := newEngine()
	setRole := func(c *gin.Context) {
		if role := c.GetHeader("X-Role"); role != "" {
			c.Set(UserContextKey, AuthUser{ID: 1, Role: role})
		}
	}
	r.GET("/admin", setRole, RequireRole("admin"), func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/staff", setRole, RequireRole("admin", "member"), func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	do := func(path, role string) int {
		req := request(http.MethodGet, path, "", "")
		if role != "" {
			req.Header.Set("X-Role", role)
		}
		return serve(r, req).Code
	}

	assert.Equal(t, http.StatusUnauthorized, do("/admin", ""))
	assert.Equal(t, http.StatusForbidden, do("/admin", "member"))
	assert.Equal(t, http.StatusOK, do("/admin", "admin"))
	assert.Equal(t, http.StatusOK, do("/staff", "member"))
	assert.Equal(t, http.StatusForbidden, do("/staff", "guest"))
}

// ---------- RateLimiter ----------

type fakeClock struct{ t time.Time }

func (f *fakeClock) now() time.Time { return f.t }

func TestRateLimiterAllow(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000, 0)}
	rl := newRateLimiter(2, time.Minute, clock.now)

	ok, _ := rl.allow("a")
	assert.True(t, ok)
	ok, _ = rl.allow("a")
	assert.True(t, ok)

	ok, retry := rl.allow("a")
	assert.False(t, ok)
	assert.Equal(t, time.Minute, retry)

	ok, _ = rl.allow("b") // IP lain tidak terpengaruh
	assert.True(t, ok)
	assert.Len(t, rl.hits, 2)

	clock.t = clock.t.Add(2 * time.Minute) // jendela lewat -> sapuan membuang entri lama
	ok, _ = rl.allow("a")
	assert.True(t, ok)
	assert.Len(t, rl.hits, 1)
}

func TestRateLimiterHandlerBlocksWith429(t *testing.T) {
	r := newEngine()
	r.Use(NewRateLimiter(1, time.Minute))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	assert.Equal(t, http.StatusOK, serve(r, request(http.MethodGet, "/ping", "", "10.0.0.1")).Code)

	w := serve(r, request(http.MethodGet, "/ping", "", "10.0.0.1"))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Contains(t, w.Body.String(), `"status":"fail"`)

	assert.Equal(t, http.StatusOK, serve(r, request(http.MethodGet, "/ping", "", "10.0.0.2")).Code)
}

func TestDefaultRateLimiterAllowsNormalTraffic(t *testing.T) {
	r := newEngine()
	r.Use(RateLimiter())
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	assert.Equal(t, http.StatusOK, serve(r, request(http.MethodGet, "/ping", "", "10.0.0.9")).Code)
}
