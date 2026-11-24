package auth

import (
	"errors"

	"gorm.io/gorm"
)

// Repository mengabstraksi akses data user (interface agar mudah diuji).
type Repository interface {
	Create(user *User) error
	FindByID(id uint) (*User, error)
	FindByUsername(username string) (*User, error)
	UsernameExists(username string, excludeID uint) (bool, error)
	EmailExists(email string, excludeID uint) (bool, error)
	Update(user *User) error
}

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

func (r *gormRepository) Create(user *User) error {
	return r.db.Create(user).Error
}

func (r *gormRepository) FindByID(id uint) (*User, error) {
	return r.first("id = ?", id)
}

// FindByUsername tidak membedakan huruf besar-kecil.
func (r *gormRepository) FindByUsername(username string) (*User, error) {
	return r.first("LOWER(username) = LOWER(?)", username)
}

func (r *gormRepository) first(query string, arg any) (*User, error) {
	var user User
	err := r.db.Where(query, arg).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *gormRepository) UsernameExists(username string, excludeID uint) (bool, error) {
	return r.exists("LOWER(username) = LOWER(?)", username, excludeID)
}

func (r *gormRepository) EmailExists(email string, excludeID uint) (bool, error) {
	return r.exists("LOWER(email) = LOWER(?)", email, excludeID)
}

func (r *gormRepository) exists(condition, value string, excludeID uint) (bool, error) {
	var count int64
	err := r.db.Model(&User{}).Where(condition, value).Where("id <> ?", excludeID).Count(&count).Error
	return count > 0, err
}

func (r *gormRepository) Update(user *User) error {
	return r.db.Save(user).Error
}
