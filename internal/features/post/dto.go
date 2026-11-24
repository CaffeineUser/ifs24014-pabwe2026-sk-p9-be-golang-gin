package post

import "time"

// PostRequest dipakai untuk membuat dan memperbarui artikel.
type PostRequest struct {
	Title          string `json:"title" example:"Belajar Golang dengan Gin"`
	Category       string `json:"category" example:"Teknologi"`
	Excerpt        string `json:"excerpt" example:"Ringkasan singkat artikel."`
	Content        string `json:"content" example:"Isi artikel dalam format Markdown."`
	CoverImagePath string `json:"cover_image_path" example:"https://example.com/cover.jpg"`
}

type AuthorResponse struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
}

// PostResponse: Content hanya terisi pada respons detail (omitempty).
// ReadingTime dalam menit.
type PostResponse struct {
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
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type StatsResponse struct {
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
	TotalPosts     int64     `json:"total_posts"`
	TotalPublished int64     `json:"total_published"`
}

func toResponse(p Post, withContent bool) PostResponse {
	res := PostResponse{
		ID:             p.ID,
		Title:          p.Title,
		Slug:           p.Slug,
		Category:       p.Category,
		Excerpt:        p.Excerpt,
		CoverImagePath: p.CoverImagePath,
		IsPublished:    p.IsPublished,
		PublishedAt:    p.PublishedAt,
		ReadingTime:    ReadingTime(p.Content),
		Author:         AuthorResponse{ID: p.User.ID, Username: p.User.Username},
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
