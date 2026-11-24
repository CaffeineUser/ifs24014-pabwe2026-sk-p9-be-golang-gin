package post

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
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"app/internal/features/auth"
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
	dsn := fmt.Sprintf("file:posttest%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", n)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&auth.User{}, &Post{}))
	return db
}

func closeDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
}

func seedUser(t *testing.T, db *gorm.DB, username, role string) auth.User {
	t.Helper()
	u := auth.User{Username: username, Email: username + "@x.id", Password: "h", Role: role}
	require.NoError(t, db.Create(&u).Error)
	return u
}

func seedPost(t *testing.T, db *gorm.DB, owner auth.User, title string, published bool) Post {
	t.Helper()
	p := Post{
		UserID: owner.ID, Title: title, Slug: Slugify(title), Category: "Teknologi",
		Excerpt: "ringkasan " + title, Content: "isi " + title, IsPublished: published,
	}
	if published {
		now := time.Now()
		p.PublishedAt = &now
	}
	require.NoError(t, db.Create(&p).Error)
	return p
}

func validRequest() PostRequest {
	return PostRequest{Title: "Belajar Golang", Category: "Teknologi", Excerpt: "ringkas", Content: "isi artikel"}
}

// flakyRepo membungkus repository asli dan bisa dipaksa gagal per method (atau semuanya).
type flakyRepo struct {
	Repository
	all    bool
	failOn map[string]bool
}

func (f *flakyRepo) boom(name string) bool { return f.all || f.failOn[name] }

func (f *flakyRepo) Create(p *Post) error {
	if f.boom("Create") {
		return errBoom
	}
	return f.Repository.Create(p)
}

func (f *flakyRepo) Update(p *Post) error {
	if f.boom("Update") {
		return errBoom
	}
	return f.Repository.Update(p)
}

func (f *flakyRepo) FindOwned(id, userID uint) (*Post, error) {
	if f.boom("FindOwned") {
		return nil, errBoom
	}
	return f.Repository.FindOwned(id, userID)
}

func (f *flakyRepo) FindPublishedBySlug(slug string) (*Post, error) {
	if f.boom("FindPublishedBySlug") {
		return nil, errBoom
	}
	return f.Repository.FindPublishedBySlug(slug)
}

func (f *flakyRepo) DeleteOwned(id, userID uint) error {
	if f.boom("DeleteOwned") {
		return errBoom
	}
	return f.Repository.DeleteOwned(id, userID)
}

func (f *flakyRepo) SlugExists(slug string, id uint) (bool, error) {
	if f.boom("SlugExists") {
		return false, errBoom
	}
	return f.Repository.SlugExists(slug, id)
}

func (f *flakyRepo) ListByUser(userID uint) ([]Post, error) {
	if f.boom("ListByUser") {
		return nil, errBoom
	}
	return f.Repository.ListByUser(userID)
}

func (f *flakyRepo) ListPublished(q, category string) ([]Post, error) {
	if f.boom("ListPublished") {
		return nil, errBoom
	}
	return f.Repository.ListPublished(q, category)
}

func (f *flakyRepo) ListAll() ([]Post, error) {
	if f.boom("ListAll") {
		return nil, errBoom
	}
	return f.Repository.ListAll()
}

func (f *flakyRepo) CountByUser(userID uint) (int64, int64, error) {
	if f.boom("CountByUser") {
		return 0, 0, errBoom
	}
	return f.Repository.CountByUser(userID)
}

func (f *flakyRepo) UserExists(id uint) (bool, error) {
	if f.boom("UserExists") {
		return false, errBoom
	}
	return f.Repository.UserExists(id)
}

func (f *flakyRepo) ListUserSummaries() ([]UserSummary, error) {
	if f.boom("ListUserSummaries") {
		return nil, errBoom
	}
	return f.Repository.ListUserSummaries()
}

func failing(names ...string) *flakyRepo {
	f := &flakyRepo{failOn: map[string]bool{}}
	for _, n := range names {
		f.failOn[n] = true
	}
	return f
}

// ---------- Model (3.2.3) ----------

func TestPostTableName(t *testing.T) {
	assert.Equal(t, "posts", Post{}.TableName())
}

func TestIsValidCategory(t *testing.T) {
	for _, c := range Categories {
		assert.True(t, IsValidCategory(c), c)
	}
	assert.False(t, IsValidCategory("Olahraga"))
	assert.False(t, IsValidCategory(""))
	assert.Equal(t, "Umum", DefaultCategory)
}

// ---------- Fungsi murni ----------

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Belajar Golang & Gin!":            "belajar-golang-gin",
		"  --Halo   Dunia--  ":             "halo-dunia",
		"Go 1.27 Dirilis":                  "go-1-27-dirilis",
		"Café Ñandú":                       "café-ñandú",
		"!!!":                              "artikel",
		"":                                 "artikel",
		strings.Repeat("a", 200):           strings.Repeat("a", 80),
		"kata " + strings.Repeat("b", 100): "kata-" + strings.Repeat("b", 75),
	}
	for input, want := range cases {
		assert.Equal(t, want, Slugify(input), input)
	}
}

func TestReadingTime(t *testing.T) {
	words := func(n int) string { return strings.TrimSpace(strings.Repeat("kata ", n)) }

	assert.Equal(t, 1, ReadingTime(""))
	assert.Equal(t, 1, ReadingTime(words(1)))
	assert.Equal(t, 1, ReadingTime(words(200)))
	assert.Equal(t, 2, ReadingTime(words(201)))
	assert.Equal(t, 2, ReadingTime(words(400)))
	assert.Equal(t, 3, ReadingTime(words(401)))
}

func TestEscapeLike(t *testing.T) {
	assert.Equal(t, `100\% asli\_ \\`, escapeLike(`100% asli_ \`))
}

func TestValidationErrorMessage(t *testing.T) {
	assert.EqualError(t, error(&ValidationError{Message: "pesan uji"}), "pesan uji")
}

// ---------- Repository ----------

func TestRepositoryQueries(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	ani := seedUser(t, db, "ani", auth.RoleMember)
	seedUser(t, db, "kosong", auth.RoleMember)

	p1 := seedPost(t, db, budi, "Belajar Golang", true)
	p2 := seedPost(t, db, budi, "Draf Rahasia", false)
	p3 := seedPost(t, db, ani, "Diskon 100% Asli", true)
	require.NoError(t, db.Model(&Post{}).Where("id = ?", p3.ID).Updates(map[string]any{"category": "Bisnis", "content": "harga_spesial"}).Error)

	t.Run("FindOwned hanya milik sendiri", func(t *testing.T) {
		found, err := repo.FindOwned(p1.ID, budi.ID)
		require.NoError(t, err)
		assert.Equal(t, "budi", found.User.Username)

		_, err = repo.FindOwned(p1.ID, ani.ID)
		assert.ErrorIs(t, err, ErrPostNotFound)
	})

	t.Run("FindPublishedBySlug menyembunyikan draf", func(t *testing.T) {
		found, err := repo.FindPublishedBySlug(p1.Slug)
		require.NoError(t, err)
		assert.Equal(t, p1.ID, found.ID)

		_, err = repo.FindPublishedBySlug(p2.Slug)
		assert.ErrorIs(t, err, ErrPostNotFound)
	})

	t.Run("SlugExists", func(t *testing.T) {
		exists, err := repo.SlugExists(p1.Slug, 0)
		require.NoError(t, err)
		assert.True(t, exists)

		exists, _ = repo.SlugExists(p1.Slug, p1.ID)
		assert.False(t, exists)
		exists, _ = repo.SlugExists("tidak-ada", 0)
		assert.False(t, exists)
	})

	t.Run("ListPublished", func(t *testing.T) {
		all, err := repo.ListPublished("", "")
		require.NoError(t, err)
		assert.Len(t, all, 2) // draf tidak ikut

		byCategory, _ := repo.ListPublished("", "Bisnis")
		require.Len(t, byCategory, 1)
		assert.Equal(t, p3.ID, byCategory[0].ID)

		caseInsensitive, _ := repo.ListPublished("GOLANG", "")
		require.Len(t, caseInsensitive, 1)
		assert.Equal(t, p1.ID, caseInsensitive[0].ID)

		inExcerpt, _ := repo.ListPublished("ringkasan belajar", "")
		assert.Len(t, inExcerpt, 1)
		inContent, _ := repo.ListPublished("SPESIAL", "")
		assert.Len(t, inContent, 1)

		percent, _ := repo.ListPublished("100%", "") // % dicari apa adanya
		assert.Len(t, percent, 1)
		onlyPercent, _ := repo.ListPublished("%", "")
		assert.Len(t, onlyPercent, 1)
		underscore, _ := repo.ListPublished("_", "")
		assert.Len(t, underscore, 1)

		none, _ := repo.ListPublished("tidak-ada-sama-sekali", "")
		assert.Empty(t, none)
	})

	t.Run("ListByUser dan ListAll", func(t *testing.T) {
		mine, err := repo.ListByUser(budi.ID)
		require.NoError(t, err)
		assert.Len(t, mine, 2) // terbit + draf

		all, err := repo.ListAll()
		require.NoError(t, err)
		assert.Len(t, all, 3)
	})

	t.Run("CountByUser", func(t *testing.T) {
		total, published, err := repo.CountByUser(budi.ID)
		require.NoError(t, err)
		assert.Equal(t, int64(2), total)
		assert.Equal(t, int64(1), published)

		total, published, _ = repo.CountByUser(999)
		assert.Equal(t, int64(0), total)
		assert.Equal(t, int64(0), published)
	})

	t.Run("UserExists", func(t *testing.T) {
		exists, err := repo.UserExists(budi.ID)
		require.NoError(t, err)
		assert.True(t, exists)
		exists, _ = repo.UserExists(999)
		assert.False(t, exists)
	})

	t.Run("ListUserSummaries", func(t *testing.T) {
		rows, err := repo.ListUserSummaries()
		require.NoError(t, err)
		require.Len(t, rows, 3)

		assert.Equal(t, "budi", rows[0].Username)
		assert.Equal(t, int64(2), rows[0].TotalPosts)
		assert.Equal(t, int64(1), rows[0].TotalPublished)
		assert.Equal(t, int64(1), rows[1].TotalPosts)
		assert.Equal(t, int64(0), rows[2].TotalPosts) // pengguna tanpa artikel tetap tampil
		assert.False(t, rows[0].CreatedAt.IsZero())
	})

	t.Run("Update tidak menyimpan relasi User", func(t *testing.T) {
		found, err := repo.FindOwned(p1.ID, budi.ID)
		require.NoError(t, err)
		found.Title = "Judul Baru"
		found.User.Username = "diubah-diam-diam"
		require.NoError(t, repo.Update(found))

		var u auth.User
		require.NoError(t, db.First(&u, budi.ID).Error)
		assert.Equal(t, "budi", u.Username)
	})

	t.Run("DeleteOwned", func(t *testing.T) {
		assert.ErrorIs(t, repo.DeleteOwned(p2.ID, ani.ID), ErrPostNotFound) // bukan milik
		assert.NoError(t, repo.DeleteOwned(p2.ID, budi.ID))
		assert.ErrorIs(t, repo.DeleteOwned(p2.ID, budi.ID), ErrPostNotFound) // sudah terhapus
	})
}

func TestRepositoryErrorsWhenDBClosed(t *testing.T) {
	db := newTestDB(t)
	repo := NewRepository(db)
	closeDB(t, db)

	assert.Error(t, repo.Create(&Post{UserID: 1, Title: "x", Slug: "x"}))
	assert.Error(t, repo.Update(&Post{ID: 1}))
	assert.Error(t, repo.DeleteOwned(1, 1))

	_, err := repo.FindOwned(1, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrPostNotFound)

	_, err = repo.FindPublishedBySlug("x")
	assert.Error(t, err)
	_, err = repo.SlugExists("x", 0)
	assert.Error(t, err)
	_, err = repo.ListByUser(1)
	assert.Error(t, err)
	_, err = repo.ListPublished("", "")
	assert.Error(t, err)
	_, err = repo.ListAll()
	assert.Error(t, err)
	_, _, err = repo.CountByUser(1)
	assert.Error(t, err)
	_, err = repo.UserExists(1)
	assert.Error(t, err)
	_, err = repo.ListUserSummaries()
	assert.Error(t, err)
}

// ---------- Service ----------

func TestServiceCreate(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	svc := NewService(NewRepository(db))

	first, err := svc.Create(budi.ID, PostRequest{Title: "  Belajar Golang  ", Content: "satu dua tiga"})
	require.NoError(t, err)
	assert.Equal(t, "belajar-golang", first.Slug)
	assert.Equal(t, DefaultCategory, first.Category) // kategori kosong -> Umum
	assert.False(t, first.IsPublished)
	assert.Nil(t, first.PublishedAt)
	assert.Equal(t, 1, first.ReadingTime)
	assert.Equal(t, "budi", first.Author.Username)
	assert.Equal(t, "satu dua tiga", first.Content)

	second, err := svc.Create(budi.ID, PostRequest{Title: "Belajar Golang"})
	require.NoError(t, err)
	assert.Equal(t, "belajar-golang-2", second.Slug)

	third, err := svc.Create(budi.ID, PostRequest{Title: "belajar golang!!!", CoverImagePath: "/uploads/a.png"})
	require.NoError(t, err)
	assert.Equal(t, "belajar-golang-3", third.Slug)
	assert.Equal(t, "/uploads/a.png", third.CoverImagePath)

	withURL, err := svc.Create(budi.ID, PostRequest{Title: "Cover URL", CoverImagePath: "https://example.com/c.jpg"})
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/c.jpg", withURL.CoverImagePath)
}

func TestServiceCreateValidation(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	svc := NewService(NewRepository(db))

	cases := []struct {
		name string
		req  PostRequest
	}{
		{"judul kosong", PostRequest{Title: "   "}},
		{"judul terlalu panjang", PostRequest{Title: strings.Repeat("a", 256)}},
		{"kategori tidak valid", PostRequest{Title: "x", Category: "Olahraga"}},
		{"excerpt terlalu panjang", PostRequest{Title: "x", Excerpt: strings.Repeat("a", 501)}},
		{"konten terlalu panjang", PostRequest{Title: "x", Content: strings.Repeat("a", 200001)}},
		{"cover skema berbahaya", PostRequest{Title: "x", CoverImagePath: "javascript:alert(1)"}},
		{"cover terlalu panjang", PostRequest{Title: "x", CoverImagePath: "/" + strings.Repeat("a", 2048)}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := svc.Create(budi.ID, tc.req)
			assert.Nil(t, res)

			var validationErr *ValidationError
			require.True(t, errors.As(err, &validationErr))
			assert.NotEmpty(t, validationErr.Message)
		})
	}
}

func TestServiceCreatePropagatesRepositoryErrors(t *testing.T) {
	for _, name := range []string{"SlugExists", "Create", "FindOwned"} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			budi := seedUser(t, db, "budi", auth.RoleMember)
			flaky := failing(name)
			flaky.Repository = NewRepository(db)

			_, err := NewService(flaky).Create(budi.ID, validRequest())
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

func TestServiceReadAndList(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	ani := seedUser(t, db, "ani", auth.RoleMember)
	mine := seedPost(t, db, budi, "Milik Budi", false)
	svc := NewService(NewRepository(db))

	got, err := svc.GetOwned(mine.ID, budi.ID)
	require.NoError(t, err)
	assert.Equal(t, "isi Milik Budi", got.Content)

	_, err = svc.GetOwned(mine.ID, ani.ID)
	assert.ErrorIs(t, err, ErrPostNotFound)

	list, err := svc.ListMine(budi.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Empty(t, list[0].Content) // daftar tanpa konten

	empty, err := svc.ListMine(ani.ID)
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Len(t, empty, 0)

	stats, err := svc.Stats(budi.ID)
	require.NoError(t, err)
	assert.Equal(t, StatsResponse{TotalPosts: 1, TotalPublished: 0, TotalDraft: 1}, *stats)
}

func TestServiceUpdate(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	ani := seedUser(t, db, "ani", auth.RoleMember)
	p := seedPost(t, db, budi, "Judul Lama", false)
	svc := NewService(NewRepository(db))

	updated, err := svc.Update(p.ID, budi.ID, PostRequest{
		Title: "Judul Baru Sama Sekali", Category: "Opini", Excerpt: "e", Content: "konten baru",
	})
	require.NoError(t, err)
	assert.Equal(t, "Judul Baru Sama Sekali", updated.Title)
	assert.Equal(t, "judul-lama", updated.Slug) // slug stabil
	assert.Equal(t, "Opini", updated.Category)
	assert.Equal(t, "konten baru", updated.Content)

	_, err = svc.Update(p.ID, ani.ID, validRequest())
	assert.ErrorIs(t, err, ErrPostNotFound)

	var validationErr *ValidationError
	_, err = svc.Update(p.ID, budi.ID, PostRequest{Title: ""})
	assert.True(t, errors.As(err, &validationErr))
}

func TestServiceUpdatePropagatesRepositoryErrors(t *testing.T) {
	for _, name := range []string{"FindOwned", "Update"} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			budi := seedUser(t, db, "budi", auth.RoleMember)
			p := seedPost(t, db, budi, "Judul", false)
			flaky := failing(name)
			flaky.Repository = NewRepository(db)

			_, err := NewService(flaky).Update(p.ID, budi.ID, validRequest())
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

func TestServiceTogglePublish(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	ani := seedUser(t, db, "ani", auth.RoleMember)
	p := seedPost(t, db, budi, "Judul", false)
	svc := NewService(NewRepository(db))

	fixed := time.Now().Truncate(time.Second)
	svc.now = func() time.Time { return fixed }

	published, err := svc.TogglePublish(p.ID, budi.ID)
	require.NoError(t, err)
	assert.True(t, published.IsPublished)
	require.NotNil(t, published.PublishedAt)
	assert.True(t, published.PublishedAt.Equal(fixed))

	draft, err := svc.TogglePublish(p.ID, budi.ID)
	require.NoError(t, err)
	assert.False(t, draft.IsPublished)
	assert.Nil(t, draft.PublishedAt)

	_, err = svc.TogglePublish(p.ID, ani.ID)
	assert.ErrorIs(t, err, ErrPostNotFound)

	blank, err := svc.Create(budi.ID, PostRequest{Title: "Kosong"})
	require.NoError(t, err)
	var validationErr *ValidationError
	_, err = svc.TogglePublish(blank.ID, budi.ID)
	assert.True(t, errors.As(err, &validationErr))
}

func TestServiceTogglePublishPropagatesRepositoryErrors(t *testing.T) {
	for _, name := range []string{"FindOwned", "Update"} {
		t.Run(name, func(t *testing.T) {
			db := newTestDB(t)
			budi := seedUser(t, db, "budi", auth.RoleMember)
			p := seedPost(t, db, budi, "Judul", false)
			flaky := failing(name)
			flaky.Repository = NewRepository(db)

			_, err := NewService(flaky).TogglePublish(p.ID, budi.ID)
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

func TestServiceDelete(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	p := seedPost(t, db, budi, "Judul", false)
	svc := NewService(NewRepository(db))

	assert.NoError(t, svc.Delete(p.ID, budi.ID))
	assert.ErrorIs(t, svc.Delete(p.ID, budi.ID), ErrPostNotFound)
}

func TestServicePublic(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	pub := seedPost(t, db, budi, "Terbit", true)
	draft := seedPost(t, db, budi, "Draf", false)
	svc := NewService(NewRepository(db))

	list, err := svc.ListPublished("  terbit ", "  ")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, pub.ID, list[0].ID)

	detail, err := svc.GetPublished(pub.Slug)
	require.NoError(t, err)
	assert.Equal(t, "isi Terbit", detail.Content)

	_, err = svc.GetPublished(draft.Slug)
	assert.ErrorIs(t, err, ErrPostNotFound)
}

func TestServiceAdmin(t *testing.T) {
	db := newTestDB(t)
	admin := seedUser(t, db, "admin", auth.RoleAdmin)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	seedPost(t, db, budi, "Satu", true)
	svc := NewService(NewRepository(db))

	users, err := svc.ListUsers()
	require.NoError(t, err)
	require.Len(t, users, 2)

	all, err := svc.ListAllPosts()
	require.NoError(t, err)
	assert.Len(t, all, 1)

	byUser, err := svc.ListPostsByUser(budi.ID)
	require.NoError(t, err)
	assert.Len(t, byUser, 1)

	noPosts, err := svc.ListPostsByUser(admin.ID)
	require.NoError(t, err)
	assert.NotNil(t, noPosts)
	assert.Len(t, noPosts, 0)

	_, err = svc.ListPostsByUser(999)
	assert.ErrorIs(t, err, auth.ErrUserNotFound)
}

func TestServicePropagatesReadErrors(t *testing.T) {
	cases := map[string]func(*Service) error{
		"ListByUser":          func(s *Service) error { _, err := s.ListMine(1); return err },
		"CountByUser":         func(s *Service) error { _, err := s.Stats(1); return err },
		"ListPublished":       func(s *Service) error { _, err := s.ListPublished("", ""); return err },
		"FindPublishedBySlug": func(s *Service) error { _, err := s.GetPublished("x"); return err },
		"ListUserSummaries":   func(s *Service) error { _, err := s.ListUsers(); return err },
		"ListAll":             func(s *Service) error { _, err := s.ListAllPosts(); return err },
		"UserExists":          func(s *Service) error { _, err := s.ListPostsByUser(1); return err },
		"DeleteOwned":         func(s *Service) error { return s.Delete(1, 1) },
	}

	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			flaky := failing(name)
			flaky.Repository = NewRepository(newTestDB(t))
			assert.ErrorIs(t, call(NewService(flaky)), errBoom)
		})
	}

	t.Run("ListByUser pada ListPostsByUser", func(t *testing.T) {
		db := newTestDB(t)
		budi := seedUser(t, db, "budi", auth.RoleMember)
		flaky := failing("ListByUser")
		flaky.Repository = NewRepository(db)

		_, err := NewService(flaky).ListPostsByUser(budi.ID)
		assert.ErrorIs(t, err, errBoom)
	})
}

// ---------- Handler (HTTP) ----------

func buildRouter(repo Repository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	authMW := middleware.Auth(testSecret)

	NewHandler(NewService(repo)).RegisterRoutes(
		r.Group("/api"),
		r.Group("/api", authMW),
		r.Group("/api/admin", authMW, middleware.RequireRole(auth.RoleAdmin)),
	)
	return r
}

func tokenFor(t *testing.T, u auth.User) string {
	t.Helper()
	claims := middleware.Claims{
		UserID: u.ID, Username: u.Username, Role: u.Role,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	require.NoError(t, err)
	return token
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

func dataOf(t *testing.T, w *httptest.ResponseRecorder) any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["data"]
}

func TestHandlerCategories(t *testing.T) {
	r := buildRouter(NewRepository(newTestDB(t)))

	w := call(r, http.MethodGet, "/api/public/categories", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Len(t, dataOf(t, w), 6)
}

func TestHandlerPublic(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	pub := seedPost(t, db, budi, "Belajar Golang", true)
	draft := seedPost(t, db, budi, "Draf Rahasia", false)
	r := buildRouter(NewRepository(db))

	w := call(r, http.MethodGet, "/api/public/posts", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	list := dataOf(t, w).([]any)
	require.Len(t, list, 1)
	item := list[0].(map[string]any)
	assert.Equal(t, "Belajar Golang", item["title"])
	assert.NotContains(t, item, "content")          // daftar tanpa konten
	assert.NotContains(t, w.Body.String(), "@x.id") // email penulis tidak bocor

	w = call(r, http.MethodGet, "/api/public/posts?q=golang&category=Teknologi", "", "")
	assert.Len(t, dataOf(t, w), 1)
	w = call(r, http.MethodGet, "/api/public/posts?q=tidak-ada", "", "")
	assert.Equal(t, []any{}, dataOf(t, w)) // [] bukan null

	w = call(r, http.MethodGet, "/api/public/posts/"+pub.Slug, "", "")
	require.Equal(t, http.StatusOK, w.Code)
	detail := dataOf(t, w).(map[string]any)
	assert.Equal(t, "isi Belajar Golang", detail["content"])
	assert.EqualValues(t, 1, detail["reading_time"])
	assert.Equal(t, "budi", detail["author"].(map[string]any)["username"])

	assert.Equal(t, http.StatusNotFound, call(r, http.MethodGet, "/api/public/posts/"+draft.Slug, "", "").Code)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodGet, "/api/public/posts/tidak-ada", "", "").Code)
}

func TestHandlerAuthorFlow(t *testing.T) {
	db := newTestDB(t)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	ani := seedUser(t, db, "ani", auth.RoleMember)
	r := buildRouter(NewRepository(db))
	tb, ta := tokenFor(t, budi), tokenFor(t, ani)

	// Tanpa token
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodGet, "/api/posts", "", "").Code)

	// Create
	w := call(r, http.MethodPost, "/api/posts", `{"title":"Artikel Pertama","content":"isi artikel"}`, tb)
	require.Equal(t, http.StatusCreated, w.Code)
	created := dataOf(t, w).(map[string]any)
	id := int(created["id"].(float64))
	assert.Equal(t, "artikel-pertama", created["slug"])
	assert.Equal(t, false, created["is_published"])

	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/posts", `{`, tb).Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPost, "/api/posts", `{"title":""}`, tb).Code)

	// List & stats (statis /stats tidak bentrok dengan /:id)
	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/posts", "", tb)), 1)
	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/posts", "", ta)), 0)

	w = call(r, http.MethodGet, "/api/posts/stats", "", tb)
	require.Equal(t, http.StatusOK, w.Code)
	stats := dataOf(t, w).(map[string]any)
	assert.EqualValues(t, 1, stats["total_posts"])
	assert.EqualValues(t, 0, stats["total_published"])
	assert.EqualValues(t, 1, stats["total_draft"])

	// Detail: milik sendiri vs milik orang lain vs ID tidak valid
	path := fmt.Sprintf("/api/posts/%d", id)
	assert.Equal(t, http.StatusOK, call(r, http.MethodGet, path, "", tb).Code)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodGet, path, "", ta).Code)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodGet, "/api/posts/abc", "", tb).Code)

	// Update
	w = call(r, http.MethodPut, path, `{"title":"Judul Diubah","category":"Opini","content":"isi baru"}`, tb)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "artikel-pertama", dataOf(t, w).(map[string]any)["slug"])
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPut, path, `{`, tb).Code)
	assert.Equal(t, http.StatusBadRequest, call(r, http.MethodPut, path, `{"title":""}`, tb).Code)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodPut, path, `{"title":"x"}`, ta).Code)

	// Toggle publish
	w = call(r, http.MethodPatch, path+"/toggle-publish", "", tb)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, true, dataOf(t, w).(map[string]any)["is_published"])
	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/public/posts", "", "")), 1)

	w = call(r, http.MethodPatch, path+"/toggle-publish", "", tb)
	assert.Equal(t, false, dataOf(t, w).(map[string]any)["is_published"])
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodPatch, path+"/toggle-publish", "", ta).Code)

	w = call(r, http.MethodPost, "/api/posts", `{"title":"Tanpa Isi"}`, tb)
	blankID := int(dataOf(t, w).(map[string]any)["id"].(float64))
	assert.Equal(t, http.StatusBadRequest,
		call(r, http.MethodPatch, fmt.Sprintf("/api/posts/%d/toggle-publish", blankID), "", tb).Code)

	// Delete
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodDelete, path, "", ta).Code)
	assert.Equal(t, http.StatusOK, call(r, http.MethodDelete, path, "", tb).Code)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodDelete, path, "", tb).Code)
}

func TestHandlerAdmin(t *testing.T) {
	db := newTestDB(t)
	admin := seedUser(t, db, "admin", auth.RoleAdmin)
	budi := seedUser(t, db, "budi", auth.RoleMember)
	seedPost(t, db, budi, "Satu", true)
	seedPost(t, db, budi, "Dua", false)
	r := buildRouter(NewRepository(db))
	ta, tb := tokenFor(t, admin), tokenFor(t, budi)

	// Member tidak boleh masuk
	assert.Equal(t, http.StatusForbidden, call(r, http.MethodGet, "/api/admin/users", "", tb).Code)
	assert.Equal(t, http.StatusUnauthorized, call(r, http.MethodGet, "/api/admin/users", "", "").Code)

	w := call(r, http.MethodGet, "/api/admin/users", "", ta)
	require.Equal(t, http.StatusOK, w.Code)
	users := dataOf(t, w).([]any)
	require.Len(t, users, 2)
	budiRow := users[1].(map[string]any)
	assert.Equal(t, "budi", budiRow["username"])
	assert.EqualValues(t, 2, budiRow["total_posts"])
	assert.EqualValues(t, 1, budiRow["total_published"])
	assert.NotContains(t, budiRow, "password")

	assert.Len(t, dataOf(t, call(r, http.MethodGet, "/api/admin/posts", "", ta)), 2) // termasuk draf

	path := fmt.Sprintf("/api/admin/users/%d/posts", budi.ID)
	assert.Len(t, dataOf(t, call(r, http.MethodGet, path, "", ta)), 2)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodGet, "/api/admin/users/999/posts", "", ta).Code)
	assert.Equal(t, http.StatusNotFound, call(r, http.MethodGet, "/api/admin/users/abc/posts", "", ta).Code)
}

func TestHandlerServerErrors(t *testing.T) {
	db := newTestDB(t)
	admin := seedUser(t, db, "admin", auth.RoleAdmin)
	r := buildRouter(&flakyRepo{Repository: NewRepository(db), all: true})
	token := tokenFor(t, admin)

	cases := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/public/posts", ""},
		{http.MethodGet, "/api/public/posts/x", ""},
		{http.MethodGet, "/api/posts", ""},
		{http.MethodPost, "/api/posts", `{"title":"x"}`},
		{http.MethodGet, "/api/posts/stats", ""},
		{http.MethodGet, "/api/posts/1", ""},
		{http.MethodPut, "/api/posts/1", `{"title":"x"}`},
		{http.MethodPatch, "/api/posts/1/toggle-publish", ""},
		{http.MethodDelete, "/api/posts/1", ""},
		{http.MethodGet, "/api/admin/users", ""},
		{http.MethodGet, "/api/admin/posts", ""},
		{http.MethodGet, "/api/admin/users/1/posts", ""},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			w := call(r, tc.method, tc.path, tc.body, token)
			assert.Equal(t, http.StatusInternalServerError, w.Code)
			assert.NotContains(t, w.Body.String(), "boom") // detail error tidak bocor
		})
	}
}
