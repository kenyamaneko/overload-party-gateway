package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestLoad_EnvOverridesDefault は環境変数が組み込みデフォルトを上書きする優先順位を検証する。
func TestLoad_EnvOverridesDefault(t *testing.T) {
	// Port は文字列パス、MatchmakingTimeoutSec は整数変換パスを通るため、両コードパスの env 優先を覆う。
	t.Setenv("PORT", "8080")
	t.Setenv("MATCHMAKING_TIMEOUT_SEC", "120")

	cfg := Load()

	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, 120, cfg.MatchmakingTimeoutSec)
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty string", "", nil},
		{"single value", "a", []string{"a"}},
		{"multiple values", "a,b,c", []string{"a", "b", "c"}},
		{"values with whitespace are trimmed", "a, b , c", []string{"a", "b", "c"}},
		{"only whitespace and commas yields empty", " , , ", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitCSV(tt.input)
			if tt.want == nil {
				assert.Empty(t, got)
			} else {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestLoad_AllowedOrigins(t *testing.T) {
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000, https://example.com")

	cfg := Load()

	assert.Equal(t, []string{"http://localhost:3000", "https://example.com"}, cfg.AllowedOrigins)
}
