package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"app/internal/middleware"
)

var errBoom = errors.New("boom")

// ---------- Helper ----------

func closedServerURL() string {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

func decodeAny(t *testing.T, raw string) any {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal([]byte(raw), &v))
	return v
}

func urlsOf(sources []Source) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, s.URL)
	}
	return out
}

type stubSearcher struct {
	sources   []Source
	err       error
	calls     int
	lastQuery string
}

func (s *stubSearcher) Search(_ context.Context, query string) ([]Source, error) {
	s.calls++
	s.lastQuery = query
	return s.sources, s.err
}

type stubGenerator struct {
	output string
	err    error
	system string
	user   string
}

func (g *stubGenerator) Complete(_ context.Context, system, user string) (string, error) {
	g.system, g.user = system, user
	return g.output, g.err
}

const validArticle = `{"title":"Judul AI","category":"Teknologi","excerpt":"Ringkasan singkat.","content":"## Isi\n\nParagraf [1]."}`

func twoSources() []Source {
	return []Source{
		{Title: "Judul [Satu]", URL: "https://example.com/a(b)", Snippet: "ringkasan\nsatu"},
		{Title: "Dua", URL: "https://example.com/b", Snippet: "dua"},
	}
}

// ---------- Parser hasil pencarian ----------

func TestParseSourcesShapes(t *testing.T) {
	cases := map[string]struct {
		payload string
		want    []string
	}{
		"array di puncak":    {`[{"title":"A","url":"https://a.id"}]`, []string{"https://a.id"}},
		"kunci results":      {`{"results":[{"title":"A","url":"https://a.id"}]}`, []string{"https://a.id"}},
		"data.results":       {`{"data":{"results":[{"title":"A","url":"https://a.id"}]}}`, []string{"https://a.id"}},
		"web.results":        {`{"web":{"results":[{"title":"A","url":"https://a.id"}]}}`, []string{"https://a.id"}},
		"kunci items":        {`{"items":[{"title":"A","link":"https://a.id"}]}`, []string{"https://a.id"}},
		"nama field lain":    {`[{"name":"A","href":"http://a.id","description":"d"}]`, []string{"http://a.id"}},
		"hasil kosong":       {`{"results":[]}`, []string{}},
		"bentuk tak dikenal": {`{"foo":"bar"}`, []string{}},
		"bukan objek/array":  {`"teks"`, []string{}},
		"terlalu dalam":      {`{"data":{"data":{"data":{"results":[{"url":"https://a.id"}]}}}}`, []string{}},
		"item bukan objek":   {`["teks",{"url":"https://a.id"}]`, []string{"https://a.id"}},
		"skema berbahaya":    {`[{"url":"javascript:alert(1)"},{"url":"ftp://x.id"},{"url":"https://ok.id"}]`, []string{"https://ok.id"}},
		"url duplikat":       {`[{"url":"https://a.id"},{"url":"https://a.id"},{"url":"https://b.id"}]`, []string{"https://a.id", "https://b.id"}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := parseSources(decodeAny(t, tc.payload), 5)
			assert.NotNil(t, got)
			assert.Equal(t, tc.want, urlsOf(got))
		})
	}
}

func TestParseSourcesDetails(t *testing.T) {
	long := strings.Repeat("x", 600)
	payload := decodeAny(t, `[{"url":"https://a.id","snippet":"`+long+`"},{"url":"https://b.id","title":"`+long+`"},{"url":"https://c.id"}]`)

	got := parseSources(payload, 2) // batas 2
	require.Len(t, got, 2)

	assert.Equal(t, "https://a.id", got[0].Title) // tanpa judul -> memakai URL
	assert.Len(t, got[0].Snippet, 500)            // snippet dipotong
	assert.Len(t, []rune(got[1].Title), 200)      // judul dipotong
}

// ---------- SearchClient ----------

func TestSearchClientPost(t *testing.T) {
	var gotAuth, gotType string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"results":[{"title":"A","url":"https://a.id","snippet":"sa"}]}`))
	}))
	defer srv.Close()

	sources, err := NewSearchClient(srv.URL, "kunci").Search(context.Background(), "golang")
	require.NoError(t, err)
	require.Len(t, sources, 1)
	assert.Equal(t, "Bearer kunci", gotAuth)
	assert.Equal(t, "application/json", gotType)
	assert.Equal(t, "golang", gotBody["query"])
	assert.Equal(t, "golang", gotBody["q"])

	_, err = NewSearchClient(srv.URL, "").Search(context.Background(), "golang") // tanpa kunci
	require.NoError(t, err)
	assert.Empty(t, gotAuth)
}

func TestSearchClientFallsBackToGet(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		gotQuery = r.URL.Query().Get("q")
		_, _ = w.Write([]byte(`[{"title":"A","url":"https://a.id"}]`))
	}))
	defer srv.Close()

	sources, err := NewSearchClient(srv.URL, "k").Search(context.Background(), "golang 1.27")
	require.NoError(t, err)
	assert.Len(t, sources, 1)
	assert.Equal(t, "golang 1.27", gotQuery)
}

func TestSearchClientErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("endpoint kosong", func(t *testing.T) {
		_, err := NewSearchClient("", "k").Search(ctx, "x")
		assert.ErrorIs(t, err, ErrSearchNotConfigured)
	})

	t.Run("endpoint tidak valid", func(t *testing.T) {
		_, err := NewSearchClient("://bad", "k").Search(ctx, "x")
		assert.Error(t, err)
	})

	t.Run("server tidak terjangkau", func(t *testing.T) {
		_, err := NewSearchClient(closedServerURL(), "k").Search(ctx, "x")
		assert.Error(t, err)
	})

	t.Run("POST dan GET sama-sama 405", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}))
		defer srv.Close()

		_, err := NewSearchClient(srv.URL, "k").Search(ctx, "x")
		assert.ErrorIs(t, err, ErrSearchUnavailable)
	})

	t.Run("status non-2xx", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		_, err := NewSearchClient(srv.URL, "k").Search(ctx, "x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("JSON tidak valid", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("bukan json"))
		}))
		defer srv.Close()

		_, err := NewSearchClient(srv.URL, "k").Search(ctx, "x")
		assert.Error(t, err)
	})
}

// ---------- OpenAIClient ----------

func TestOpenAIClientSuccess(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"jawaban"}}]}`))
	}))
	defer srv.Close()

	client := NewOpenAIClient(srv.URL+"/", "kunci", "model-x") // garis miring ganda dirapikan
	out, err := client.Complete(context.Background(), "sistem", "pengguna")

	require.NoError(t, err)
	assert.Equal(t, "jawaban", out)
	assert.Equal(t, "/chat/completions", gotPath)
	assert.Equal(t, "Bearer kunci", gotAuth)
	assert.Equal(t, "model-x", gotBody.Model)
	require.Len(t, gotBody.Messages, 2)
	assert.Equal(t, "sistem", gotBody.Messages[0].Content)
	assert.Equal(t, "pengguna", gotBody.Messages[1].Content)
	assert.Equal(t, completionMaxTokens, gotBody.MaxTokens)
}

func TestOpenAIClientErrors(t *testing.T) {
	ctx := context.Background()

	serve := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}

	t.Run("belum dikonfigurasi", func(t *testing.T) {
		for _, c := range []*OpenAIClient{
			NewOpenAIClient("", "k", "m"),
			NewOpenAIClient("http://x", "", "m"),
			NewOpenAIClient("http://x", "k", ""),
		} {
			_, err := c.Complete(ctx, "s", "u")
			assert.ErrorIs(t, err, ErrNotConfigured)
		}
	})

	t.Run("endpoint tidak valid", func(t *testing.T) {
		_, err := NewOpenAIClient("://bad", "k", "m").Complete(ctx, "s", "u")
		assert.Error(t, err)
	})

	t.Run("server tidak terjangkau", func(t *testing.T) {
		_, err := NewOpenAIClient(closedServerURL(), "k", "m").Complete(ctx, "s", "u")
		assert.Error(t, err)
	})

	t.Run("status non-2xx dengan pesan penyedia", func(t *testing.T) {
		srv := serve(http.StatusUnauthorized, `{"error":{"message":"kunci salah"}}`)
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "401")
		assert.Contains(t, err.Error(), "kunci salah")
	})

	t.Run("status non-2xx dengan body bukan JSON", func(t *testing.T) {
		srv := serve(http.StatusInternalServerError, "gangguan")
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "500")
	})

	t.Run("200 tetapi JSON tidak valid", func(t *testing.T) {
		srv := serve(http.StatusOK, "bukan json")
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tidak valid")
	})

	t.Run("tanpa choices", func(t *testing.T) {
		srv := serve(http.StatusOK, `{"choices":[]}`)
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		assert.Error(t, err)
	})

	t.Run("konten kosong", func(t *testing.T) {
		srv := serve(http.StatusOK, `{"choices":[{"message":{"content":"   "}}]}`)
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		assert.Error(t, err)
	})

	t.Run("status 429 bertipe UpstreamError (error berbentuk array)", func(t *testing.T) {
		srv := serve(http.StatusTooManyRequests, `[{"error":{"code":429,"message":"kuota habis"}}]`)
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		var upstreamErr *UpstreamError
		require.True(t, errors.As(err, &upstreamErr))
		assert.Equal(t, http.StatusTooManyRequests, upstreamErr.StatusCode)
		assert.Contains(t, upstreamErr.Error(), "kuota habis")
	})

	t.Run("status non-2xx dengan body kosong", func(t *testing.T) {
		srv := serve(http.StatusBadGateway, "")
		defer srv.Close()

		_, err := NewOpenAIClient(srv.URL, "k", "m").Complete(ctx, "s", "u")
		require.Error(t, err)
		assert.Equal(t, "layanan AI mengembalikan status 502", err.Error())
	})
}

// ---------- Service ----------

func TestServiceGenerateWithSearch(t *testing.T) {
	searcher := &stubSearcher{sources: twoSources()}
	gen := &stubGenerator{output: validArticle}

	res, err := NewService(searcher, gen).Generate(context.Background(),
		GenerateRequest{Topic: "  Golang 1.27 ", Category: "Pendidikan"})
	require.NoError(t, err)

	assert.Equal(t, "Judul AI", res.Title)
	assert.Equal(t, "Pendidikan", res.Category) // pilihan penulis diutamakan
	assert.Equal(t, "Ringkasan singkat.", res.Excerpt)
	assert.Empty(t, res.SearchWarning)
	assert.Len(t, res.SearchSources, 2)

	assert.Equal(t, "Golang 1.27", searcher.lastQuery)
	assert.Contains(t, gen.user, "Topik artikel: Golang 1.27")
	assert.Contains(t, gen.user, "Kategori yang diminta: Pendidikan")
	assert.Contains(t, gen.user, "[1] Judul [Satu]")
	assert.Contains(t, gen.user, "Ringkasan: ringkasan satu") // baris baru dirapikan
	assert.Contains(t, gen.system, "Teknologi, Pendidikan")

	assert.Contains(t, res.Content, "Paragraf [1].")
	assert.Contains(t, res.Content, "## Referensi")
	assert.Contains(t, res.Content, `1. [Judul \[Satu\]](https://example.com/a%28b%29)`)
	assert.Contains(t, res.Content, "2. [Dua](https://example.com/b)")
	assert.False(t, strings.HasSuffix(res.Content, "\n"))
}

func TestServiceGenerateWithoutSearch(t *testing.T) {
	disabled := false
	searcher := &stubSearcher{sources: twoSources()}
	gen := &stubGenerator{output: validArticle}

	res, err := NewService(searcher, gen).Generate(context.Background(),
		GenerateRequest{Topic: "Topik uji", IncludeWebSearch: &disabled})
	require.NoError(t, err)

	assert.Equal(t, 0, searcher.calls)
	assert.NotNil(t, res.SearchSources)
	assert.Len(t, res.SearchSources, 0)
	assert.Empty(t, res.SearchWarning)
	assert.NotContains(t, res.Content, "## Referensi")
	assert.NotContains(t, gen.user, "Hasil penelusuran web")
	assert.Contains(t, gen.user, "Pilih kategori yang paling sesuai") // kategori kosong -> AI memilih
	assert.Equal(t, "Teknologi", res.Category)                        // kategori valid dari AI
}

func TestServiceGenerateSearchProblemsAreNotFatal(t *testing.T) {
	cases := map[string]*stubSearcher{
		"pencarian gagal": {err: errBoom},
		"hasil kosong":    {sources: []Source{}},
	}

	for name, searcher := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := NewService(searcher, &stubGenerator{output: validArticle}).
				Generate(context.Background(), GenerateRequest{Topic: "Topik uji"})

			require.NoError(t, err)
			assert.NotEmpty(t, res.SearchWarning)
			assert.NotNil(t, res.SearchSources)
			assert.Len(t, res.SearchSources, 0)
			assert.NotContains(t, res.Content, "## Referensi")
		})
	}
}

func TestServiceGenerateValidation(t *testing.T) {
	svc := NewService(&stubSearcher{}, &stubGenerator{output: validArticle})

	cases := map[string]GenerateRequest{
		"topik kosong":          {Topic: "  "},
		"topik terlalu pendek":  {Topic: "ab"},
		"topik terlalu panjang": {Topic: strings.Repeat("a", 201)},
		"kategori tidak valid":  {Topic: "Topik uji", Category: "Olahraga"},
	}

	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := svc.Generate(context.Background(), req)
			assert.Nil(t, res)

			var validationErr *ValidationError
			require.True(t, errors.As(err, &validationErr))
			assert.NotEmpty(t, validationErr.Error())
		})
	}
}

func TestServiceGenerateFailures(t *testing.T) {
	req := GenerateRequest{Topic: "Topik uji"}
	noSearch := &stubSearcher{sources: []Source{}}

	t.Run("AI belum dikonfigurasi", func(t *testing.T) {
		_, err := NewService(noSearch, &stubGenerator{err: ErrNotConfigured}).Generate(context.Background(), req)
		assert.ErrorIs(t, err, ErrNotConfigured)
		assert.NotErrorIs(t, err, ErrGeneration)
	})

	t.Run("penyedia AI gagal", func(t *testing.T) {
		_, err := NewService(noSearch, &stubGenerator{err: errBoom}).Generate(context.Background(), req)
		assert.ErrorIs(t, err, ErrGeneration)
	})

	outputs := map[string]string{
		"bukan JSON":      "maaf, saya tidak bisa",
		"kurung terbalik": "} salah {",
		"JSON rusak":      `{"title": }`,
		"tanpa judul":     `{"title":" ","content":"isi"}`,
		"tanpa konten":    `{"title":"Judul","content":""}`,
	}
	for name, output := range outputs {
		t.Run(name, func(t *testing.T) {
			_, err := NewService(noSearch, &stubGenerator{output: output}).Generate(context.Background(), req)
			assert.ErrorIs(t, err, ErrGeneration)
		})
	}
}

func TestServiceGenerateKeepsUpstreamError(t *testing.T) {
	gen := &stubGenerator{err: &UpstreamError{StatusCode: http.StatusTooManyRequests}}

	_, err := NewService(&stubSearcher{sources: []Source{}}, gen).
		Generate(context.Background(), GenerateRequest{Topic: "Topik uji"})

	var upstreamErr *UpstreamError
	require.True(t, errors.As(err, &upstreamErr))
	assert.ErrorIs(t, err, ErrGeneration)
}

func TestServiceGenerateNormalizesOutput(t *testing.T) {
	noSearch := &stubSearcher{sources: []Source{}}
	req := GenerateRequest{Topic: "Topik uji"}

	t.Run("JSON berpagar kode dan teks pembuka", func(t *testing.T) {
		out := "Berikut hasilnya:\n```json\n" + validArticle + "\n```"
		res, err := NewService(noSearch, &stubGenerator{output: out}).Generate(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "Judul AI", res.Title)
	})

	t.Run("kategori AI tidak valid -> Umum", func(t *testing.T) {
		out := `{"title":"J","category":"Olahraga","content":"isi"}`
		res, err := NewService(noSearch, &stubGenerator{output: out}).Generate(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "Umum", res.Category)
	})

	t.Run("judul dan excerpt dipotong", func(t *testing.T) {
		out := `{"title":"` + strings.Repeat("j", 300) + `","excerpt":"` + strings.Repeat("e", 600) + `","content":"isi"}`
		res, err := NewService(noSearch, &stubGenerator{output: out}).Generate(context.Background(), req)
		require.NoError(t, err)
		assert.Len(t, res.Title, 255)
		assert.Len(t, res.Excerpt, 500)
	})
}

// ---------- Handler ----------

func buildRouter(searcher Searcher, generator Generator, limiter gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewHandler(NewService(searcher, generator)).RegisterRoutes(r.Group("/api"), limiter)
	return r
}

func noLimit(c *gin.Context) { c.Next() }

func postJSON(r *gin.Engine, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/posts/ai-generate", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestHandlerGenerateSuccess(t *testing.T) {
	r := buildRouter(&stubSearcher{sources: twoSources()}, &stubGenerator{output: validArticle}, noLimit)

	w := postJSON(r, `{"topic":"Golang 1.27","category":"Teknologi","include_web_search":true}`)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	data := body["data"].(map[string]any)
	assert.Equal(t, "Judul AI", data["title"])
	assert.Len(t, data["search_sources"], 2)
	assert.NotContains(t, data, "search_warning")
}

func TestHandlerSourcesAlwaysArray(t *testing.T) {
	r := buildRouter(&stubSearcher{}, &stubGenerator{output: validArticle}, noLimit)

	w := postJSON(r, `{"topic":"Golang 1.27","include_web_search":false}`)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, []any{}, body["data"].(map[string]any)["search_sources"]) // [] bukan null
}

func TestHandlerErrors(t *testing.T) {
	good := &stubGenerator{output: validArticle}

	t.Run("JSON rusak", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, postJSON(buildRouter(&stubSearcher{}, good, noLimit), `{`).Code)
	})

	t.Run("validasi gagal", func(t *testing.T) {
		assert.Equal(t, http.StatusBadRequest, postJSON(buildRouter(&stubSearcher{}, good, noLimit), `{"topic":""}`).Code)
	})

	t.Run("AI belum dikonfigurasi -> 503", func(t *testing.T) {
		r := buildRouter(&stubSearcher{}, &stubGenerator{err: ErrNotConfigured}, noLimit)
		assert.Equal(t, http.StatusServiceUnavailable, postJSON(r, `{"topic":"Topik uji"}`).Code)
	})

	t.Run("penyedia AI gagal -> 502 tanpa bocor detail", func(t *testing.T) {
		r := buildRouter(&stubSearcher{}, &stubGenerator{err: errBoom}, noLimit)
		w := postJSON(r, `{"topic":"Topik uji"}`)
		assert.Equal(t, http.StatusBadGateway, w.Code)
		assert.NotContains(t, w.Body.String(), "boom")
	})

	t.Run("kuota AI habis -> 429", func(t *testing.T) {
		gen := &stubGenerator{err: &UpstreamError{StatusCode: http.StatusTooManyRequests, Detail: ": rahasia-penyedia"}}
		w := postJSON(buildRouter(&stubSearcher{}, gen, noLimit), `{"topic":"Topik uji"}`)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), `"status":"fail"`)
		assert.NotContains(t, w.Body.String(), "rahasia-penyedia")
	})
}

func TestHandlerIsRateLimited(t *testing.T) {
	r := buildRouter(&stubSearcher{}, &stubGenerator{output: validArticle}, middleware.NewRateLimiter(1, time.Minute))

	assert.Equal(t, http.StatusOK, postJSON(r, `{"topic":"Topik uji"}`).Code)
	assert.Equal(t, http.StatusTooManyRequests, postJSON(r, `{"topic":"Topik uji"}`).Code)
}
