package ai

// GenerateRequest: include_web_search kosong dianggap true.
type GenerateRequest struct {
	Topic            string `json:"topic" example:"Dampak AI pada pendidikan tinggi di Indonesia"`
	Category         string `json:"category" example:"Pendidikan"`
	IncludeWebSearch *bool  `json:"include_web_search" example:"true"`
}

// Source adalah satu sumber rujukan dari mesin pencari.
type Source struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// GenerateResponse: SearchSources selalu array; SearchWarning hanya ada bila pencarian bermasalah.
type GenerateResponse struct {
	Title         string   `json:"title"`
	Category      string   `json:"category"`
	Excerpt       string   `json:"excerpt"`
	Content       string   `json:"content"`
	SearchSources []Source `json:"search_sources"`
	SearchWarning string   `json:"search_warning,omitempty"`
}
