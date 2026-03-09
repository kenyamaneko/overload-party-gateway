package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	cfg := Load()

	assert.Equal(t, "9001", cfg.Port)
	assert.Equal(t, "dev", cfg.Env)
	assert.Equal(t, "info", cfg.LogLevel)
	assert.Equal(t, "", cfg.DatabaseURL)
	assert.Equal(t, "", cfg.AppleKeyID)
	assert.Equal(t, "", cfg.AppleIssuerID)
	assert.Equal(t, "", cfg.AppleBundleID)
	assert.Equal(t, "", cfg.ApplePrivateKeyPath)
	assert.Equal(t, "Sandbox", cfg.AppleEnvironment)
	assert.Equal(t, "", cfg.GooglePackageName)
	assert.Nil(t, cfg.AllowedOrigins)
	assert.Equal(t, "http://localhost:9002", cfg.BattleServerURL)
}

func TestLoad_CustomValues(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("ENV", "production")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("DATABASE_URL", "postgres://localhost:5432/mydb")
	t.Setenv("APPLE_KEY_ID", "key123")
	t.Setenv("APPLE_ISSUER_ID", "issuer456")
	t.Setenv("APPLE_BUNDLE_ID", "com.example.app")
	t.Setenv("APPLE_PRIVATE_KEY_PATH", "/path/to/key.p8")
	t.Setenv("APPLE_ENVIRONMENT", "Production")
	t.Setenv("GOOGLE_PACKAGE_NAME", "com.example.android")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("BATTLE_SERVER_URL", "http://battle:9002")

	cfg := Load()

	assert.Equal(t, "8080", cfg.Port)
	assert.Equal(t, "production", cfg.Env)
	assert.Equal(t, "debug", cfg.LogLevel)
	assert.Equal(t, "postgres://localhost:5432/mydb", cfg.DatabaseURL)
	assert.Equal(t, "key123", cfg.AppleKeyID)
	assert.Equal(t, "issuer456", cfg.AppleIssuerID)
	assert.Equal(t, "com.example.app", cfg.AppleBundleID)
	assert.Equal(t, "/path/to/key.p8", cfg.ApplePrivateKeyPath)
	assert.Equal(t, "Production", cfg.AppleEnvironment)
	assert.Equal(t, "com.example.android", cfg.GooglePackageName)
	assert.Equal(t, []string{"http://localhost:3000"}, cfg.AllowedOrigins)
	assert.Equal(t, "http://battle:9002", cfg.BattleServerURL)
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
		{
			name:  "single value",
			input: "a",
			want:  []string{"a"},
		},
		{
			name:  "multiple values",
			input: "a,b,c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "values with whitespace are trimmed",
			input: "a, b , c",
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "only whitespace and commas yields empty",
			input: " , , ",
			want:  nil,
		},
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
