package docs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSwaggerJSON(t *testing.T) {
	body, err := GetSwaggerJSON("api.example.com:9000", "https")
	require.NoError(t, err)

	var spec map[string]any
	require.NoError(t, json.Unmarshal(body, &spec))

	assert.Equal(t, "api.example.com:9000", spec["host"])
	assert.Equal(t, []any{"https"}, spec["schemes"])
	assert.Equal(t, "/api", spec["basePath"])

	paths := spec["paths"].(map[string]any)
	for _, p := range []string{"/auth/login", "/public/posts/{slug}", "/posts/ai-generate", "/admin/users"} {
		assert.Contains(t, paths, p)
	}
}

func TestGetSwaggerJSONInvalidDoc(t *testing.T) {
	original := readDoc
	defer func() { readDoc = original }()
	readDoc = func() string { return "bukan json" }

	_, err := GetSwaggerJSON("localhost", "http")
	assert.Error(t, err)
}
