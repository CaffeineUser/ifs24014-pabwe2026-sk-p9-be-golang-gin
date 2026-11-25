package post

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"app/internal/features/auth"
)

type Repository interface {
	Create(p *Post) error
	Update(p *Post) error
	FindByID(id uint) (*Post, error)
	FindOwned(id, userID uint) (*Post, error)
	FindPublishedBySlug(slug string) (*Post, error)
	Delete(id uint) error
	DeleteOwned(id, userID uint) error
	SlugExists(slug string, excludeID uint) (bool, error)
	ListByUser(userID uint) ([]Post, error)
	ListPublished(q, category string) ([]Post, error)
	ListAll() ([]Post, error)
	CountByUser(userID uint) (total, published int64, err error)
	UserExists(id uint) (bool, error)
	ListUserSummaries() ([]UserSummary, error)
}

type gormRepository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) Repository {
	return &gormRepository{db: db}
}

type countRow struct {
	Total     int64
	Published int64
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// escapeLike menetralkan wildcard LIKE agar kata kunci dicari apa adanya.
func escapeLike(s string) string {
	return likeEscaper.Replace(s)
}

func (r *gormRepository) Create(p *Post) error {
	return r.db.Omit("User").Create(p).Error
}

func (r *gormRepository) Update(p *Post) error {
	return r.db.Omit("User").Save(p).Error
}

func (r *gormRepository) FindByID(id uint) (*Post, error) {
	return r.first(r.db.Where("id = ?", id))
}

// FindOwned hanya menemukan artikel milik userID (artikel orang lain = tidak ditemukan).
func (r *gormRepository) FindOwned(id, userID uint) (*Post, error) {
	return r.first(r.db.Where("id = ? AND user_id = ?", id, userID))
}

func (r *gormRepository) Delete(id uint) error {
	res := r.db.Delete(&Post{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPostNotFound
	}
	return nil
}

func (r *gormRepository) FindPublishedBySlug(slug string) (*Post, error) {
	return r.first(r.db.Where("slug = ? AND is_published = ?", slug, true))
}

func (r *gormRepository) first(query *gorm.DB) (*Post, error) {
	var p Post
	err := query.Preload("User").First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPostNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *gormRepository) DeleteOwned(id, userID uint) error {
	res := r.db.Where("id = ? AND user_id = ?", id, userID).Delete(&Post{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPostNotFound
	}
	return nil
}

func (r *gormRepository) SlugExists(slug string, excludeID uint) (bool, error) {
	var count int64
	err := r.db.Model(&Post{}).Where("slug = ?", slug).Where("id <> ?", excludeID).Count(&count).Error
	return count > 0, err
}

func (r *gormRepository) list(query *gorm.DB) ([]Post, error) {
	var posts []Post
	err := query.Preload("User").Find(&posts).Error
	return posts, err
}

func (r *gormRepository) ListByUser(userID uint) ([]Post, error) {
	return r.list(r.db.Where("user_id = ?", userID).Order("created_at DESC, id DESC"))
}

func (r *gormRepository) ListAll() ([]Post, error) {
	return r.list(r.db.Order("created_at DESC, id DESC"))
}

// ListPublished: hanya artikel terbit; q mencari di judul/excerpt/konten tanpa peka huruf besar-kecil.
func (r *gormRepository) ListPublished(q, category string) ([]Post, error) {
	tx := r.db.Where("is_published = ?", true).Order("published_at DESC, id DESC")

	if category != "" {
		tx = tx.Where("category = ?", category)
	}
	if q != "" {
		like := "%" + escapeLike(strings.ToLower(q)) + "%"
		tx = tx.Where(
			`(LOWER(title) LIKE ? ESCAPE '\' OR LOWER(excerpt) LIKE ? ESCAPE '\' OR LOWER(content) LIKE ? ESCAPE '\')`,
			like, like, like,
		)
	}
	return r.list(tx)
}

func (r *gormRepository) CountByUser(userID uint) (int64, int64, error) {
	var row countRow
	err := r.db.Model(&Post{}).
		Where("user_id = ?", userID).
		Select("COUNT(*) AS total, COUNT(CASE WHEN is_published THEN 1 END) AS published").
		Scan(&row).Error
	return row.Total, row.Published, err
}

func (r *gormRepository) UserExists(id uint) (bool, error) {
	var count int64
	err := r.db.Model(&auth.User{}).Where("id = ?", id).Count(&count).Error
	return count > 0, err
}

func (r *gormRepository) ListUserSummaries() ([]UserSummary, error) {
	var rows []UserSummary
	err := r.db.Table("users").
		Select("users.id, users.username, users.email, users.role, users.created_at, " +
			"COUNT(posts.id) AS total_posts, " +
			"COUNT(posts.id) AS total_articles, " +
			"COUNT(CASE WHEN posts.is_published THEN 1 END) AS total_published, " +
			"COUNT(CASE WHEN posts.is_published THEN 1 END) AS total_selesai").
		Joins("LEFT JOIN posts ON posts.user_id = users.id").
		Group("users.id").
		Order("users.id").
		Scan(&rows).Error
	return rows, err
}
