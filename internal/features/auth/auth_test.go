package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"app/internal/middleware"
)

const testSecret = "rahasia-uji-minimal-32-karakter-aman!!"

var (
	errBoom   = errors.New("boom")
	dbCounter uint64
)

// ---------- Helper ----------

// DB SQLite dibuat langsung (bukan lewat paket database) untuk menghindari import cycle.
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	n := atomic.AddUint64(&dbCounter, 1)
	dsn := fmt.Sprintf("file:authtest%d?mode=memory&cache=shared", n)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&User{}))
	return db
}

func newService(repo Repository) *Service {
	svc := NewService(repo, testSecret, time.Hour)
	svc.cost = bcrypt.MinCost
	return svc
}

// flakyRepo membungkus repository asli dan bisa dipaksa gagal per method.
type flakyRepo struct {
	Repository
	failCreate, failUpdate, failFindByID, failFindByUsername bool
	failFindByEmail                                          bool
	failUsernameExists, failEmailExists                      bool
}

func (f *flakyRepo) Create(u *User) error {
	if f.failCreate {
		return errBoom
	}
	return f.Repository.Create(u)
}

func (f *flakyRepo) Update(u *User) error {
	if f.failUpdate {
		return errBoom
	}
	return f.Repository.Update(u)
}

func (f *flakyRepo) FindByID(id uint) (*User, error) {
	if f.failFindByID {
		return nil, errBoom
	}
	return f.Repository.FindByID(id)
}

func (f *flakyRepo) FindByUsername(name string) (*User, error) {
	if f.failFindByUsername {
		return nil, errBoom
	}
	return f.Repository.FindByUsername(name)
}

func (f *flakyRepo) FindByEmail(email string) (*User, error) {
	if f.failFindByEmail {
		return nil, errBoom
	}
	return f.Repository.FindByEmail(email)
}

func (f *flakyRepo) UsernameExists(name string, id uint) (bool, error) {
	if f.failUsernameExists {
		return false, errBoom
	}
	return f.Repository.UsernameExists(name, id)
}

func (f *flakyRepo) EmailExists(email string, id uint) (bool, error) {
	if f.failEmailExists {
		return false, errBoom
	}
	return f.Repository.EmailExists(email, id)
}

func validRegister() RegisterRequest {
	return RegisterRequest{Username: "budi_01", Email: "budi@example.com", Password: "rahasia123"}
}

// ---------- Model (3.2.3) ----------

func TestUserTableName(t *testing.T) {
	assert.Equal(t, "users", User{}.TableName())
}

func TestRoleConstants(t *testing.T) {
	assert.Equal(t, "member", RoleMember)
	assert.Equal(t, "admin", RoleAdmin)
}

// ---------- Repository ----------

func TestRepository(t *testing.T) {
	repo := NewRepository(newTestDB(t))
	u := &User{Username: "Budi", Email: "budi@x.id", Password: "h", Role: RoleMember}
	require.NoError(t, repo.Create(u))
	require.NotZero(t, u.ID)

	byID, err := repo.FindByID(u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Budi", byID.Username)

	byName, err := repo.FindByUsername("bUDI")
	require.NoError(t, err)
	assert.Equal(t, u.ID, byName.ID)

	_, err = repo.FindByID(999)
	assert.ErrorIs(t, err, ErrUserNotFound)
	_, err = repo.FindByUsername("nobody")
	assert.ErrorIs(t, err, ErrUserNotFound)

	exists, err := repo.UsernameExists("BUDI", 0)
	require.NoError(t, err)
	assert.True(t, exists)
	exists, _ = repo.UsernameExists("budi", u.ID) // diri sendiri dikecualikan
	assert.False(t, exists)
	exists, _ = repo.UsernameExists("lain", 0)
	assert.False(t, exists)

	exists, err = repo.EmailExists("BUDI@X.ID", 0)
	require.NoError(t, err)
	assert.True(t, exists)
	exists, _ = repo.EmailExists("budi@x.id", u.ID)
	assert.False(t, exists)

	u.Username = "Budi2"
	require.NoError(t, repo.Update(u))
	reloaded, err := repo.FindByID(u.ID)
	require.NoError(t, err)
	assert.Equal(t, "Budi2", reloaded.Username)
}

func TestRepositoryErrorsWhenDBClosed(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	assert.Error(t, repo.Create(&User{Username: "x", Email: "x@x.id", Password: "h"}))
	assert.Error(t, repo.Update(&User{ID: 1}))

	_, err = repo.FindByID(1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrUserNotFound)

	_, err = repo.UsernameExists("x", 0)
	assert.Error(t, err)
	_, err = repo.EmailExists("x@x.id", 0)
	assert.Error(t, err)
}

// ---------- Service: Register ----------

func TestRegisterSuccess(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))

	res, err := svc.Register(RegisterRequest{Username: "  budi_01 ", Email: " Budi@Example.COM ", Password: "rahasia123"})
	require.NoError(t, err)

	assert.Equal(t, "budi_01", res.User.Username)
	assert.Equal(t, "budi@example.com", res.User.Email)
	assert.Equal(t, RoleMember, res.User.Role)
	assert.NotEmpty(t, res.Token)
}

func TestRegisterValidation(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))

	cases := []struct {
		name string
		req  RegisterRequest
	}{
		{"username kosong", RegisterRequest{"", "a@b.co", "rahasia123"}},
		{"username terlalu pendek", RegisterRequest{"ab", "a@b.co", "rahasia123"}},
		{"username berisi spasi", RegisterRequest{"bud i", "a@b.co", "rahasia123"}},
		{"username terlalu panjang", RegisterRequest{strings.Repeat("a", 31), "a@b.co", "rahasia123"}},
		{"email salah", RegisterRequest{"budi", "bukan-email", "rahasia123"}},
		{"email tanpa titik pada domain", RegisterRequest{"budi", "a@b", "rahasia123"}},
		{"email terlalu panjang", RegisterRequest{"budi", strings.Repeat("a", 251) + "@x.id", "rahasia123"}},
		{"password terlalu pendek", RegisterRequest{"budi", "a@b.co", "1234567"}},
		{"password lebih dari 72 byte", RegisterRequest{"budi", "a@b.co", strings.Repeat("a", 73)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.Register(tc.req)
			assert.Nil(t, res)

			var validationErr *ValidationError
			require.True(t, errors.As(err, &validationErr))
			assert.NotEmpty(t, validationErr.Message)
		})
	}
}

func TestRegisterDuplicates(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))
	_, err := svc.Register(validRegister())
	require.NoError(t, err)

	_, err = svc.Register(RegisterRequest{Username: "BUDI_01", Email: "lain@example.com", Password: "rahasia123"})
	assert.ErrorIs(t, err, ErrUsernameTaken)

	_, err = svc.Register(RegisterRequest{Username: "orang_lain", Email: "BUDI@example.com", Password: "rahasia123"})
	assert.ErrorIs(t, err, ErrEmailTaken)
}

func TestRegisterPropagatesRepositoryErrors(t *testing.T) {
	cases := map[string]*flakyRepo{
		"cek username": {failUsernameExists: true},
		"cek email":    {failEmailExists: true},
		"simpan":       {failCreate: true},
	}

	for name, flaky := range cases {
		t.Run(name, func(t *testing.T) {
			flaky.Repository = NewRepository(newTestDB(t))
			_, err := newService(flaky).Register(validRegister())
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

func TestValidationErrorMessage(t *testing.T) {
	err := error(&ValidationError{Message: "pesan uji"})
	assert.EqualError(t, err, "pesan uji")
}

// ---------- Service: Login ----------

func TestLoginSuccessAndTokenClaims(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))
	registered, err := svc.Register(validRegister())
	require.NoError(t, err)

	fixed := time.Now().Truncate(time.Second)
	svc.now = func() time.Time { return fixed }

	result, err := svc.Login(LoginRequest{Username: "BUDI_01", Password: "rahasia123"}) // case-insensitive
	require.NoError(t, err)
	assert.Equal(t, registered.User.ID, result.User.ID)
	assert.Equal(t, RoleMember, result.User.Role)

	// Login dengan email juga berhasil
	resultEmail, err := svc.Login(LoginRequest{Username: "budi@example.com", Password: "rahasia123"})
	require.NoError(t, err)
	assert.Equal(t, registered.User.ID, resultEmail.User.ID)

	claims := &middleware.Claims{}
	_, err = jwt.ParseWithClaims(result.Token, claims,
		func(*jwt.Token) (any, error) { return []byte(testSecret), nil },
		jwt.WithTimeFunc(func() time.Time { return fixed }),
	)
	require.NoError(t, err)
	assert.Equal(t, registered.User.ID, claims.UserID)
	assert.Equal(t, "budi_01", claims.Username)
	assert.Equal(t, RoleMember, claims.Role)
	assert.True(t, claims.ExpiresAt.Time.Equal(fixed.Add(time.Hour)))
}

func TestLoginFailures(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))
	_, err := svc.Register(validRegister())
	require.NoError(t, err)

	for _, req := range []LoginRequest{{"", "x"}, {"budi_01", ""}, {"   ", "x"}} {
		_, err = svc.Login(req)
		var validationErr *ValidationError
		assert.True(t, errors.As(err, &validationErr))
	}

	_, err = svc.Login(LoginRequest{Username: "tidak_ada", Password: "rahasia123"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)

	_, err = svc.Login(LoginRequest{Username: "budi_01", Password: "salah-total"})
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginPropagatesRepositoryError(t *testing.T) {
	flaky := &flakyRepo{Repository: NewRepository(newTestDB(t)), failFindByUsername: true}
	_, err := newService(flaky).Login(LoginRequest{Username: "budi", Password: "x"})
	assert.ErrorIs(t, err, errBoom)
}

func TestLoginFailsWhenTokenCannotBeSigned(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))
	_, err := svc.Register(validRegister())
	require.NoError(t, err)

	svc.key = "bukan-slice-byte" // HMAC mensyaratkan []byte
	_, err = svc.Login(LoginRequest{Username: "budi_01", Password: "rahasia123"})
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrInvalidCredentials)
}

// ---------- Service: Profile ----------

func TestGetProfile(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))
	registered, err := svc.Register(validRegister())
	require.NoError(t, err)

	user, err := svc.GetProfile(registered.User.ID)
	require.NoError(t, err)
	assert.Equal(t, "budi_01", user.Username)

	_, err = svc.GetProfile(999)
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestUpdateProfile(t *testing.T) {
	svc := newService(NewRepository(newTestDB(t)))
	budi, err := svc.Register(validRegister())
	require.NoError(t, err)
	_, err = svc.Register(RegisterRequest{Username: "ani", Email: "ani@example.com", Password: "rahasia123"})
	require.NoError(t, err)

	// Mengirim ulang data milik sendiri diperbolehkan.
	same, err := svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Username: "budi_01", Email: "budi@example.com"})
	require.NoError(t, err)
	assert.Equal(t, "budi_01", same.Username)

	updated, err := svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Username: " Budi_Baru ", Email: "Baru@Example.com"})
	require.NoError(t, err)
	assert.Equal(t, "Budi_Baru", updated.Username)
	assert.Equal(t, "baru@example.com", updated.Email)
	assert.Equal(t, RoleMember, updated.Role)

	// Update password
	withPwd, err := svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Password: "passwordbaru123"})
	require.NoError(t, err)
	assert.NotNil(t, withPwd)
	loginWithNewPwd, err := svc.Login(LoginRequest{Username: "Budi_Baru", Password: "passwordbaru123"})
	require.NoError(t, err)
	assert.NotEmpty(t, loginWithNewPwd.Token)

	_, err = svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Username: "ANI", Email: "baru@example.com"})
	assert.ErrorIs(t, err, ErrUsernameTaken)
	_, err = svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Username: "budi_baru", Email: "ANI@example.com"})
	assert.ErrorIs(t, err, ErrEmailTaken)

	var validationErr *ValidationError
	_, err = svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Username: "x", Email: "a@b.co"})
	assert.True(t, errors.As(err, &validationErr))
	_, err = svc.UpdateProfile(budi.User.ID, UpdateProfileRequest{Username: "budi_baru", Email: "salah"})
	assert.True(t, errors.As(err, &validationErr))

	_, err = svc.UpdateProfile(999, UpdateProfileRequest{Username: "budi_baru", Email: "a@b.co"})
	assert.ErrorIs(t, err, ErrUserNotFound)
}

func TestUpdateProfilePropagatesRepositoryErrors(t *testing.T) {
	cases := map[string]*flakyRepo{
		"cari user":    {failFindByID: true},
		"cek username": {failUsernameExists: true},
		"cek email":    {failEmailExists: true},
		"simpan":       {failUpdate: true},
	}

	for name, flaky := range cases {
		t.Run(name, func(t *testing.T) {
			base := NewRepository(newTestDB(t))
			seed, err := newService(base).Register(validRegister())
			require.NoError(t, err)

			flaky.Repository = base
			_, err = newService(flaky).UpdateProfile(seed.User.ID, UpdateProfileRequest{Username: "budi_baru", Email: "baru@example.com"})
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

// ---------- Handler (HTTP) ----------

func buildRouter(repo Repository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(newService(repo)).RegisterRoutes(r.Group("/api"), r.Group("/api", middleware.Auth(testSecret)))
	return r
}

func call(r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body
}

const budiJSON = `{"username":"budi","email":"budi@x.id","password":"rahasia123"}`

func registerAndLogin(t *testing.T, r *gin.Engine) string {
	t.Helper()
	require.Equal(t, http.StatusCreated, call(r, http.MethodPost, "/api/auth/register", budiJSON, "").Code)

	w := call(r, http.MethodPost, "/api/auth/login", `{"username":"budi","password":"rahasia123"}`, "")
	require.Equal(t, http.StatusOK, w.Code)
	data := decodeBody(t, w)["data"].(map[string]any)
	return data["token"].(string)
}

func TestHandlerRegister(t *testing.T) {
	r := buildRouter(NewRepository(newTestDB(t)))

	// Klien mencoba menyisipkan role admin -> harus diabaikan.
	w := call(r, http.MethodPost, "/api/auth/register",
		`{"username":"budi","email":"budi@x.id","password":"rahasia123","role":"admin"}`, "")
	assert.Equal(t, http.StatusCreated, w.Code)
	body := decodeBody(t, w)
	assert.Equal(t, "success", body["status"])
	data := body["data"].(map[string]any)
	assert.Equal(t, "member", data["user"].(map[string]any)["role"])
	assert.NotEmpty(t, data["token"])
	assert.NotContains(t, w.Body.String(), "password")

	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/auth/register", `{`, "").Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/auth/register", `{"username":"a"}`, "").Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/auth/register", budiJSON, "").Code)
}

func TestHandlerRegisterServerError(t *testing.T) {
	r := buildRouter(&flakyRepo{Repository: NewRepository(newTestDB(t)), failCreate: true})

	w := call(r, http.MethodPost, "/api/auth/register", budiJSON, "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "error", decodeBody(t, w)["status"])
	assert.NotContains(t, w.Body.String(), "boom") // detail error tidak bocor
}

func TestHandlerLogin(t *testing.T) {
	r := buildRouter(NewRepository(newTestDB(t)))
	token := registerAndLogin(t, r)
	assert.NotEmpty(t, token)

	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/auth/login", `{`, "").Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/auth/login", `{"username":""}`, "").Code)
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodPost, "/api/auth/login", `{"username":"budi","password":"salah"}`, "").Code)
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodPost, "/api/auth/login", `{"username":"hantu","password":"salah"}`, "").Code)
}

func TestHandlerLoginServerError(t *testing.T) {
	r := buildRouter(&flakyRepo{Repository: NewRepository(newTestDB(t)), failFindByUsername: true})
	w := call(r, http.MethodPost, "/api/auth/login", `{"username":"budi","password":"x"}`, "")
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandlerLogout(t *testing.T) {
	r := buildRouter(NewRepository(newTestDB(t)))
	token := registerAndLogin(t, r)

	assert.Equal(t, http.StatusOK, call(r, http.MethodPost, "/api/auth/logout", "", token).Code)
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodPost, "/api/auth/logout", "", "").Code)
}

func TestHandlerProfile(t *testing.T) {
	db := newTestDB(t)
	r := buildRouter(NewRepository(db))
	token := registerAndLogin(t, r)

	w := call(r, http.MethodGet, "/api/auth/profile", "", token)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "budi", decodeBody(t, w)["data"].(map[string]any)["username"])

	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodGet, "/api/auth/profile", "", "").Code)

	// Akun dihapus tetapi token masih berlaku -> 401.
	require.NoError(t, db.Delete(&User{}, 1).Error)
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodGet, "/api/auth/profile", "", token).Code)
}

func TestHandlerUpdateProfile(t *testing.T) {
	r := buildRouter(NewRepository(newTestDB(t)))
	token := registerAndLogin(t, r)
	require.Equal(t, http.StatusCreated, call(r, http.MethodPost, "/api/auth/register",
		`{"username":"ani","email":"ani@x.id","password":"rahasia123"}`, "").Code)

	w := call(r, http.MethodPut, "/api/auth/profile", `{"username":"budi_baru","email":"baru@x.id"}`, token)
	require.Equal(t, http.StatusOK, w.Code)
	data := decodeBody(t, w)["data"].(map[string]any)
	assert.Equal(t, "budi_baru", data["username"])
	assert.Equal(t, "member", data["role"])

	w = call(r, http.MethodGet, "/api/auth/profile", "", token)
	assert.Equal(t, "baru@x.id", decodeBody(t, w)["data"].(map[string]any)["email"])

	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPut, "/api/auth/profile", `{`, token).Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPut, "/api/auth/profile", `{"username":"x","email":"a@b.co"}`, token).Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPut, "/api/auth/profile", `{"username":"ANI","email":"baru@x.id"}`, token).Code)
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodPut, "/api/auth/profile", `{"username":"budi_baru","email":"baru@x.id"}`, "").Code)
}
