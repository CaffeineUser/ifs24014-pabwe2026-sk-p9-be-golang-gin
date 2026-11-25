package post

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	wordsPerMinute   = 200
	maxTitleLength   = 255
	maxExcerptLength = 500
	maxContentBytes  = 200000
	maxCoverLength   = 2048
	maxSlugLength    = 80
	fallbackSlug     = "artikel"
)

var (
	ErrPostNotFound = errors.New("Artikel tidak ditemukan")
	ErrForbidden    = errors.New("Anda tidak memiliki akses ke artikel ini")
)

// ValidationError dipetakan handler menjadi HTTP 400.
type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string { return e.Message }

// Slugify mengubah judul menjadi slug URL (huruf & angka dipisah tanda hubung).
func Slugify(title string) string {
	var b strings.Builder
	pendingHyphen := false

	for _, r := range strings.ToLower(title) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			pendingHyphen = true
			continue
		}
		if pendingHyphen && b.Len() > 0 {
			b.WriteByte('-')
		}
		pendingHyphen = false
		b.WriteRune(r)
		if b.Len() >= maxSlugLength {
			break
		}
	}

	if b.Len() == 0 {
		return fallbackSlug
	}
	return b.String()
}

// ReadingTime menghitung estimasi menit baca (kata / 200 wpm, dibulatkan ke atas, minimal 1).
func ReadingTime(content string) int {
	words := len(strings.Fields(content))
	return max(1, (words+wordsPerMinute-1)/wordsPerMinute)
}

func validCoverPath(path string) bool {
	return len(path) <= maxCoverLength &&
		(path == "" || strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "/"))
}

// normalize merapikan input dan memvalidasinya.
func normalize(req PostRequest) (PostRequest, error) {
	req.Title = strings.TrimSpace(req.Title)
	req.Category = strings.TrimSpace(req.Category)
	req.Excerpt = strings.TrimSpace(req.Excerpt)
	req.Content = strings.TrimSpace(req.Content)
	req.CoverImagePath = strings.TrimSpace(req.CoverImagePath)

	if req.Category == "" {
		req.Category = DefaultCategory
	}

	switch {
	case req.Title == "" || utf8.RuneCountInString(req.Title) > maxTitleLength:
		return req, &ValidationError{Message: "Judul wajib diisi dan maksimal 255 karakter"}
	case req.Content == "":
		return req, &ValidationError{Message: "Konten wajib diisi"}
	case !IsValidCategory(req.Category):
		return req, &ValidationError{Message: "Kategori tidak valid. Pilihan: " + strings.Join(Categories, ", ")}
	case utf8.RuneCountInString(req.Excerpt) > maxExcerptLength:
		return req, &ValidationError{Message: "Excerpt maksimal 500 karakter"}
	case len(req.Content) > maxContentBytes:
		return req, &ValidationError{Message: "Konten terlalu panjang"}
	case !validCoverPath(req.CoverImagePath):
		return req, &ValidationError{Message: "Path gambar sampul harus berupa URL http(s) atau path yang diawali /"}
	}
	return req, nil
}

type Service struct {
	repo Repository
	now  func() time.Time
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// uniqueSlug menambahkan akhiran -2, -3, ... sampai slug belum dipakai.
func (s *Service) uniqueSlug(base string, excludeID uint) (string, error) {
	slug := base
	for n := 2; ; n++ {
		exists, err := s.repo.SlugExists(slug, excludeID)
		if err != nil {
			return "", err
		}
		if !exists {
			return slug, nil
		}
		slug = fmt.Sprintf("%s-%d", base, n)
	}
}

func (s *Service) detailOwned(id, userID uint) (*PostResponse, error) {
	post, err := s.repo.FindOwned(id, userID)
	if err != nil {
		return nil, err
	}
	res := toResponse(*post, true)
	return &res, nil
}

// ---------- Penulis ----------

func (s *Service) Create(userID uint, req PostRequest) (*PostResponse, error) {
	req, err := normalize(req)
	if err != nil {
		return nil, err
	}

	slug, err := s.uniqueSlug(Slugify(req.Title), 0)
	if err != nil {
		return nil, err
	}

	isPublished := true
	if req.IsPublished != nil {
		isPublished = *req.IsPublished
	}
	var publishedAt *time.Time
	if isPublished {
		now := s.now()
		publishedAt = &now
	}

	post := &Post{
		UserID:         userID,
		Title:          req.Title,
		Slug:           slug,
		Category:       req.Category,
		Excerpt:        req.Excerpt,
		Content:        req.Content,
		CoverImagePath: req.CoverImagePath,
		IsPublished:    isPublished,
		PublishedAt:    publishedAt,
	}
	if err := s.repo.Create(post); err != nil {
		return nil, err
	}
	return s.detailOwned(post.ID, userID)
}

func (s *Service) ListMine(userID uint) ([]PostResponse, error) {
	posts, err := s.repo.ListByUser(userID)
	if err != nil {
		return nil, err
	}
	return toSummaries(posts), nil
}

func (s *Service) GetOwned(id, userID uint) (*PostResponse, error) {
	post, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if post.UserID != userID {
		return nil, ErrForbidden
	}
	res := toResponse(*post, true)
	return &res, nil
}

// Update tidak mengubah slug agar tautan lama tetap berlaku.
func (s *Service) Update(id, userID uint, req PostRequest) (*PostResponse, error) {
	req, err := normalize(req)
	if err != nil {
		return nil, err
	}

	post, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if post.UserID != userID {
		return nil, ErrForbidden
	}

	post.Title = req.Title
	post.Category = req.Category
	post.Excerpt = req.Excerpt
	post.Content = req.Content
	post.CoverImagePath = req.CoverImagePath
	if req.IsPublished != nil {
		post.IsPublished = *req.IsPublished
		if post.IsPublished && post.PublishedAt == nil {
			now := s.now()
			post.PublishedAt = &now
		} else if !post.IsPublished {
			post.PublishedAt = nil
		}
	}
	if err := s.repo.Update(post); err != nil {
		return nil, err
	}

	res := toResponse(*post, true)
	return &res, nil
}

// TogglePublish membalik status Draf <-> Terbit beserta published_at.
func (s *Service) TogglePublish(id, userID uint) (*PostResponse, error) {
	post, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	if post.UserID != userID {
		return nil, ErrForbidden
	}

	if post.IsPublished {
		post.IsPublished = false
		post.PublishedAt = nil
	} else {
		if strings.TrimSpace(post.Content) == "" {
			return nil, &ValidationError{Message: "Artikel tanpa konten tidak dapat diterbitkan"}
		}
		now := s.now()
		post.IsPublished = true
		post.PublishedAt = &now
	}

	if err := s.repo.Update(post); err != nil {
		return nil, err
	}
	res := toResponse(*post, true)
	return &res, nil
}

func (s *Service) Delete(id, userID uint) error {
	post, err := s.repo.FindByID(id)
	if err != nil {
		return err
	}
	if post.UserID != userID {
		return ErrForbidden
	}
	return s.repo.Delete(id)
}

func (s *Service) Stats(userID uint) (*StatsResponse, error) {
	total, published, err := s.repo.CountByUser(userID)
	if err != nil {
		return nil, err
	}
	draft := total - published
	return &StatsResponse{
		TotalArticles:  total,
		TotalSelesai:   published,
		BelumSelesai:   draft,
		TotalPosts:     total,
		TotalPublished: published,
		TotalDraft:     draft,
	}, nil
}

// ---------- Publik ----------

func (s *Service) ListPublished(q, category string) ([]PostResponse, error) {
	posts, err := s.repo.ListPublished(strings.TrimSpace(q), strings.TrimSpace(category))
	if err != nil {
		return nil, err
	}
	return toSummaries(posts), nil
}

func (s *Service) GetPublished(slug string) (*PostResponse, error) {
	post, err := s.repo.FindPublishedBySlug(slug)
	if err != nil {
		return nil, err
	}
	res := toResponse(*post, true)
	return &res, nil
}

// ---------- Admin ----------

func (s *Service) ListUsers() ([]UserSummary, error) {
	rows, err := s.repo.ListUserSummaries()
	if err != nil {
		return nil, err
	}
	out := make([]UserSummary, len(rows)) // non-nil -> JSON []
	copy(out, rows)
	return out, nil
}

func (s *Service) ListAllPosts() ([]PostResponse, error) {
	posts, err := s.repo.ListAll()
	if err != nil {
		return nil, err
	}
	return toSummaries(posts), nil
}

func (s *Service) ListPostsByUser(userID uint) ([]PostResponse, error) {
	posts, err := s.repo.ListByUser(userID)
	if err != nil {
		return nil, err
	}
	return toSummaries(posts), nil
}
