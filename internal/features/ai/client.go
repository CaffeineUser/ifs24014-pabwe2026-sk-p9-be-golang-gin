package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	completionTemperature = 0.7
	// Model "thinking" menghitung token berpikir dalam batas ini, jadi dibuat longgar.
	completionMaxTokens = 8192
)

var ErrNotConfigured = errors.New("Fitur AI belum dikonfigurasi")

// UpstreamError menandai respons non-2xx dari layanan AI.
type UpstreamError struct {
	StatusCode int
	Detail     string
}

func (e *UpstreamError) Error() string {
	return fmt.Sprintf("layanan AI mengembalikan status %d%s", e.StatusCode, e.Detail)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// OpenAIClient memanggil endpoint Chat Completions yang kompatibel dengan OpenAI
// (OpenAI, Gemini lewat /v1beta/openai, dan sejenisnya).
type OpenAIClient struct {
	endpoint string
	apiKey   string
	model    string
	http     *http.Client
}

func NewOpenAIClient(endpoint, apiKey, model string) *OpenAIClient {
	return &OpenAIClient{
		endpoint: endpoint,
		apiKey:   apiKey,
		model:    model,
		http:     &http.Client{Timeout: 120 * time.Second},
	}
}

// upstreamDetail mengambil potongan isi respons error untuk diagnosis (bentuk JSON bebas).
func upstreamDetail(raw []byte) string {
	detail := truncateRunes(oneLine(string(raw)), 300)
	if detail == "" {
		return ""
	}
	return ": " + detail
}

// Complete mengirim prompt dan mengembalikan teks jawaban model.
func (c *OpenAIClient) Complete(ctx context.Context, system, user string) (string, error) {
	if c.endpoint == "" || c.apiKey == "" || c.model == "" {
		return "", ErrNotConfigured
	}

	payload, _ := json.Marshal(chatRequest{
		Model: c.model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: completionTemperature,
		MaxTokens:   completionMaxTokens,
	})

	url := strings.TrimRight(c.endpoint, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", &UpstreamError{StatusCode: resp.StatusCode, Detail: upstreamDetail(raw)}
	}

	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("respons AI tidak valid: %w", err)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("respons AI kosong")
	}
	return out.Choices[0].Message.Content, nil
}
