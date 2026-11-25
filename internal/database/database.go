package database

import (
	"fmt"
	"sync/atomic"

	"github.com/glebarez/sqlite"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"app/internal/config"
	"app/internal/features/auth"
	"app/internal/features/post"
)

var memCounter uint64

func Connect(cfg config.Config) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{})
}

func NewSQLiteMemory() (*gorm.DB, error) {
	n := atomic.AddUint64(&memCounter, 1)
	dsn := fmt.Sprintf("file:mem%d?mode=memory&cache=shared&_pragma=foreign_keys(1)", n)
	return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
}

// users harus dimigrasikan sebelum posts.
func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&auth.User{}, &post.Post{})
}

// Seed idempoten: aman dijalankan berkali-kali.
func Seed(db *gorm.DB, cfg config.Config) error {
	accounts := []auth.User{
		{Username: cfg.AdminUsername, Email: cfg.AdminUsername + "@delcom.org", Password: cfg.AdminPassword, Role: auth.RoleAdmin},
		{Username: "member", Email: "member@delcom.org", Password: "member", Role: auth.RoleMember},
	}

	for _, acc := range accounts {
		hash, err := bcrypt.GenerateFromPassword([]byte(acc.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		acc.Password = string(hash)

		var existing auth.User
		if err := db.Where("username = ?", acc.Username).First(&existing).Error; err != nil {
			if err := db.Create(&acc).Error; err != nil {
				return err
			}
		} else {
			existing.Role = acc.Role
			existing.Password = acc.Password
			existing.Email = acc.Email
			if err := db.Save(&existing).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
