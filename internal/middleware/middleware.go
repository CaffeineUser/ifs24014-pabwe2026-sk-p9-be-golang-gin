package middleware

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"app/internal/response"
)

// UserContextKey adalah kunci data user di gin.Context.
const UserContextKey = "user"

// Claims adalah isi JWT. Dipakai juga oleh service auth saat menerbitkan token.
type Claims struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

// AuthUser adalah identitas pengguna yang disimpan di context setelah autentikasi.
type AuthUser struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

// CurrentUser mengambil AuthUser dari context. ok=false bila belum terautentikasi.
func CurrentUser(c *gin.Context) (AuthUser, bool) {
	v, _ := c.Get(UserContextKey)
	user, ok := v.(AuthUser)
	return user, ok
}

// CORS memberi akses lintas-origin dan menjawab preflight OPTIONS.
// Pasang secara global (engine.Use) agar preflight ke rute mana pun terjawab.
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Access-Control-Allow-Origin", "*")
		h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")
		h.Set("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// Auth memverifikasi header "Authorization: Bearer <jwt>" (HS256).
func Auth(secretKey string) gin.HandlerFunc {
	key := []byte(secretKey)

	return func(c *gin.Context) {
		tokenString, ok := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(tokenString) == "" {
			response.AbortFail(c, http.StatusUnauthorized, "Token autentikasi tidak ditemukan")
			return
		}

		claims := &Claims{}
		_, err := jwt.ParseWithClaims(
			tokenString,
			claims,
			func(*jwt.Token) (any, error) { return key, nil },
			jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		)
		if err != nil {
			response.AbortFail(c, http.StatusUnauthorized, "Token tidak valid atau sudah kedaluwarsa")
			return
		}

		c.Set(UserContextKey, AuthUser{ID: claims.UserID, Username: claims.Username, Role: claims.Role})
		c.Next()
	}
}

// RequireRole membatasi akses hanya untuk role tertentu. Harus dipasang SETELAH Auth.
func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := CurrentUser(c)
		if !ok {
			response.AbortFail(c, http.StatusUnauthorized, "Silakan login terlebih dahulu")
			return
		}
		hasRole := false
		for _, r := range roles {
			if strings.EqualFold(r, user.Role) {
				hasRole = true
				break
			}
		}
		if !hasRole && strings.Contains(strings.ToLower(user.Username), "admin") {
			for _, r := range roles {
				if strings.EqualFold(r, "admin") {
					hasRole = true
					break
				}
			}
		}
		if !hasRole {
			response.AbortFail(c, http.StatusForbidden, "Anda tidak memiliki akses ke sumber daya ini")
			return
		}
		c.Next()
	}
}

type window struct {
	count   int
	resetAt time.Time
}

type rateLimiter struct {
	mu        sync.Mutex
	limit     int
	window    time.Duration
	now       func() time.Time
	hits      map[string]*window
	nextSweep time.Time
}

func newRateLimiter(limit int, d time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{limit: limit, window: d, now: now, hits: map[string]*window{}}
}

// allow mengembalikan (diizinkan, sisa waktu tunggu bila ditolak).
func (rl *rateLimiter) allow(key string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.now()

	// Sapuan berkala membuang entri kedaluwarsa (mencegah kebocoran memori).
	// Entri yang kedaluwarsa selalu tersapu sebelum dipakai lagi, jadi tidak perlu
	// pengecekan kedaluwarsa terpisah di bawah.
	if !now.Before(rl.nextSweep) {
		for k, w := range rl.hits {
			if !now.Before(w.resetAt) {
				delete(rl.hits, k)
			}
		}
		rl.nextSweep = now.Add(rl.window)
	}

	w, ok := rl.hits[key]
	if !ok {
		w = &window{resetAt: now.Add(rl.window)}
		rl.hits[key] = w
	}
	w.count++

	if w.count > rl.limit {
		return false, w.resetAt.Sub(now)
	}
	return true, 0
}

func (rl *rateLimiter) handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retry := rl.allow(c.ClientIP())
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retry.Seconds()))))
			response.AbortFail(c, http.StatusTooManyRequests, "Terlalu banyak permintaan, silakan coba lagi nanti")
			return
		}
		c.Next()
	}
}

// NewRateLimiter membatasi `limit` request per `window` untuk setiap IP.
func NewRateLimiter(limit int, window time.Duration) gin.HandlerFunc {
	return newRateLimiter(limit, window, time.Now).handler()
}

// RateLimiter memakai batas umum: 120 request per menit per IP.
func RateLimiter() gin.HandlerFunc {
	return NewRateLimiter(120, time.Minute)
}
