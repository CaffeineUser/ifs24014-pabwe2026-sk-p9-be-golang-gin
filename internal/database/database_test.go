package database

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"app/internal/config"
	"app/internal/features/auth"
	"app/internal/features/post"
)

func testConfig() config.Config {
	return config.Config{AdminUsername: "admin", AdminPassword: "admin"}
}

func migratedDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := NewSQLiteMemory()
	require.NoError(t, err)
	require.NoError(t, Migrate(db))
	return db
}

func TestConnectFailsWhenPostgresUnreachable(t *testing.T) {
	cfg := config.Config{DBHost: "127.0.0.1", DBPort: "1", DBUsername: "x", DBPassword: "x", DBDatabase: "x", DBSSLMode: "disable"}
	_, err := Connect(cfg)
	assert.Error(t, err)
}

func TestMigrateCreatesTables(t *testing.T) {
	db := migratedDB(t)
	assert.True(t, db.Migrator().HasTable("users"))
	assert.True(t, db.Migrator().HasTable("posts"))
}

func TestSeedIsIdempotentAndHashesPasswords(t *testing.T) {
	db := migratedDB(t)

	require.NoError(t, Seed(db, testConfig()))
	require.NoError(t, Seed(db, testConfig()))

	var users []auth.User
	require.NoError(t, db.Order("id").Find(&users).Error)
	require.Len(t, users, 2)

	assert.Equal(t, "admin", users[0].Username)
	assert.Equal(t, auth.RoleAdmin, users[0].Role)
	assert.Equal(t, "member", users[1].Username)
	assert.Equal(t, auth.RoleMember, users[1].Role)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(users[0].Password), []byte("admin")))
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(users[1].Password), []byte("member")))
}

func TestSeedFailsWithoutTables(t *testing.T) {
	db, err := NewSQLiteMemory()
	require.NoError(t, err)
	assert.Error(t, Seed(db, testConfig()))
}

func TestSeedFailsWhenPasswordTooLong(t *testing.T) {
	db := migratedDB(t)
	cfg := testConfig()
	cfg.AdminPassword = strings.Repeat("a", 73)
	assert.Error(t, Seed(db, cfg))
}

func TestPostSlugMustBeUnique(t *testing.T) {
	db := migratedDB(t)
	u := auth.User{Username: "u", Email: "u@x.id", Password: "p"}
	require.NoError(t, db.Create(&u).Error)

	require.NoError(t, db.Create(&post.Post{UserID: u.ID, Title: "A", Slug: "a"}).Error)
	assert.Error(t, db.Create(&post.Post{UserID: u.ID, Title: "B", Slug: "a"}).Error)
}

func TestDefaultsAndCascadeDelete(t *testing.T) {
	db := migratedDB(t)
	u := auth.User{Username: "u", Email: "u@x.id", Password: "p"}
	require.NoError(t, db.Create(&u).Error)

	var gotUser auth.User
	require.NoError(t, db.First(&gotUser, u.ID).Error)
	assert.Equal(t, auth.RoleMember, gotUser.Role)

	p := post.Post{UserID: u.ID, Title: "Halo", Slug: "halo"}
	require.NoError(t, db.Create(&p).Error)

	var saved post.Post
	require.NoError(t, db.Preload("User").First(&saved, p.ID).Error)
	assert.Equal(t, post.DefaultCategory, saved.Category)
	assert.False(t, saved.IsPublished)
	assert.Nil(t, saved.PublishedAt)
	assert.Equal(t, "u", saved.User.Username)

	require.NoError(t, db.Model(&saved).Updates(map[string]any{"is_published": true, "published_at": time.Now()}).Error)

	require.NoError(t, db.Delete(&auth.User{}, u.ID).Error)
	var count int64
	require.NoError(t, db.Model(&post.Post{}).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}

func TestPostRequiresExistingUser(t *testing.T) {
	db := migratedDB(t)
	assert.Error(t, db.Create(&post.Post{UserID: 999, Title: "x", Slug: "x"}).Error)
}
