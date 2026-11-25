package post

import "time"

// PostRequest dipakai untuk membuat dan memperbarui artikel.
type PostRequest struct {
	Title          string `json:"title" example:"Belajar Golang dengan Gin"`
	Category       string `json:"category" example:"Teknologi"`
	Excerpt        string `json:"excerpt" example:"Ringkasan singkat artikel."`
	Content        string `json:"content" example:"Isi artikel dalam format Markdown."`
	CoverImagePath string `json:"cover_image_path" example:"https://example.com/cover.jpg"`
	IsPublished    *bool  `json:"is_published" example:"true"`
}

type AuthorResponse struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
}

// PostResponse: Content hanya terisi pada respons detail (omitempty).
// ReadingTime dalam menit.
type PostResponse struct {
	ID             uint           `json:"id"`
	UserID         uint           `json:"user_id"`
	Title          string         `json:"title"`
	Slug           string         `json:"slug"`
	Category       string         `json:"category"`
	Excerpt        string         `json:"excerpt"`
	Content        string         `json:"content,omitempty"`
	CoverImagePath string         `json:"cover_image_path"`
	IsPublished    bool           `json:"is_published"`
	PublishedAt    *time.Time     `json:"published_at"`
	ReadingTime    int            `json:"reading_time"`
	Author         AuthorResponse `json:"author"`
	AuthorName     string         `json:"author_name"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

// PublicPostResponse tidak menyertakan user_id atau informasi internal akun.
type PublicPostResponse struct {
	ID             uint           `json:"id"`
	Title          string         `json:"title"`
	Slug           string         `json:"slug"`
	Category       string         `json:"category"`
	Excerpt        string         `json:"excerpt"`
	Content        string         `json:"content,omitempty"`
	CoverImagePath string         `json:"cover_image_path"`
	IsPublished    bool           `json:"is_published"`
	PublishedAt    *time.Time     `json:"published_at"`
	ReadingTime    int            `json:"reading_time"`
	Author         AuthorResponse `json:"author"`
	AuthorName     string         `json:"author_name"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type StatsResponse struct {
	TotalArticles  int64 `json:"total_articles"`
	TotalSelesai   int64 `json:"total_selesai"`
	BelumSelesai   int64 `json:"belum_selesai"`
	TotalPosts     int64 `json:"total_posts"`
	TotalPublished int64 `json:"total_published"`
	TotalDraft     int64 `json:"total_draft"`
}

// UserSummary adalah baris monitoring admin (hasil query agregat).
type UserSummary struct {
	ID             uint      `json:"id"`
	Username       string    `json:"username"`
	Email          string    `json:"email"`
	Role           string    `json:"role"`
	CreatedAt      time.Time `json:"created_at"`
	TotalArticles  int64     `json:"total_articles"`
	TotalPosts     int64     `json:"total_posts"`
	TotalSelesai   int64     `json:"total_selesai"`
	TotalPublished int64     `json:"total_published"`
}

func toResponse(p Post, withContent bool) PostResponse {
	username := p.User.Username
	res := PostResponse{
		ID:             p.ID,
		UserID:         p.UserID,
		Title:          p.Title,
		Slug:           p.Slug,
		Category:       p.Category,
		Excerpt:        p.Excerpt,
		CoverImagePath: p.CoverImagePath,
		IsPublished:    p.IsPublished,
		PublishedAt:    p.PublishedAt,
		ReadingTime:    ReadingTime(p.Content),
		AuthorName:     p.User.Username,
		Author:         AuthorResponse{ID: p.User.ID, Username: username},
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
	if withContent {
		res.Content = p.Content
	}
	return res
}

// toSummaries selalu mengembalikan slice non-nil agar JSON menjadi [] (bukan null).
func toSummaries(posts []Post) []PostResponse {
	out := make([]PostResponse, 0, len(posts))
	for _, p := range posts {
		out = append(out, toResponse(p, false))
	}
	return out
}

func toPublicResponse(p Post, withContent bool) PublicPostResponse {
	username := p.User.Username
	res := PublicPostResponse{
		ID:             p.ID,
		Title:          p.Title,
		Slug:           p.Slug,
		Category:       p.Category,
		Excerpt:        p.Excerpt,
		CoverImagePath: p.CoverImagePath,
		IsPublished:    p.IsPublished,
		PublishedAt:    p.PublishedAt,
		ReadingTime:    ReadingTime(p.Content),
		AuthorName:     username,
		Author:         AuthorResponse{ID: p.User.ID, Username: username},
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
	if withContent {
		res.Content = p.Content
	}
	return res
}

func toPublicSummaries(posts []Post) []PublicPostResponse {
	out := make([]PublicPostResponse, 0, len(posts))
	for _, p := range posts {
		out = append(out, toPublicResponse(p, false))
	}
	return out
}
