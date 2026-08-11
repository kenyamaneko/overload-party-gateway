package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testBattleServerURL        = "http://localhost:19002"
	testCardServiceURL         = "http://localhost:19003"
	testMatchmakingServiceURL  = "http://localhost:19004"
	testAccountServiceURL      = "http://localhost:19005"
	testShopServiceURL         = "http://localhost:19006"
	testScenarioServiceURL     = "http://localhost:19007"
	testNewsServiceURL         = "http://localhost:19008"
	testSupportServiceURL      = "http://localhost:19009"
	testDatabaseConn           = "postgresql://test:test@localhost:15432/gateway_test?sslmode=disable"
	testInternalAuthPrivateKey = "test-internal-auth-private-key"
)

func requiredEnv() map[string]string {
	return map[string]string{
		"BATTLE_SERVER_URL":         testBattleServerURL,
		"CARD_SERVICE_URL":          testCardServiceURL,
		"MATCHMAKING_SERVICE_URL":   testMatchmakingServiceURL,
		"ACCOUNT_SERVICE_URL":       testAccountServiceURL,
		"SHOP_SERVICE_URL":          testShopServiceURL,
		"SCENARIO_SERVICE_URL":      testScenarioServiceURL,
		"NEWS_SERVICE_URL":          testNewsServiceURL,
		"SUPPORT_SERVICE_URL":       testSupportServiceURL,
		"DATABASE_CONN":             testDatabaseConn,
		"INTERNAL_AUTH_PRIVATE_KEY": testInternalAuthPrivateKey,
	}
}

func setValidRequiredEnv(t *testing.T) {
	t.Helper()
	for key, value := range requiredEnv() {
		t.Setenv(key, value)
	}
}

func setValidRequiredEnvExcept(t *testing.T, excludedKey string) {
	t.Helper()
	for key, value := range requiredEnv() {
		if key == excludedKey {
			continue
		}
		t.Setenv(key, value)
	}
	unsetEnv(t, excludedKey)
}

// 実行環境にたまたま key が設定されていると「未設定」ケースの結果が変わってしまうため、明示的に unset してから復元する。
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	original, wasSet := os.LookupEnv(key)
	require.NoError(t, os.Unsetenv(key))
	t.Cleanup(func() {
		if wasSet {
			require.NoError(t, os.Setenv(key, original))
			return
		}
		require.NoError(t, os.Unsetenv(key))
	})
}

func TestFromEnv(t *testing.T) {
	t.Run("[設定]設定値の既定値解決", func(t *testing.T) {
		t.Run("対応する環境変数が未設定のとき、既定値が使われて設定の構築は成功する", func(t *testing.T) {
			setValidRequiredEnv(t)
			unsetEnv(t, "PORT")

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "9001", cfg.Port)
		})

		t.Run("対応する環境変数が空文字列として設定されているとき、設定の構築はエラーになる(未設定と空文字列を区別する)", func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("PORT", "")

			_, err := FromEnv()

			require.Error(t, err)
		})

		t.Run("対応する環境変数に値が設定されているとき、その値が使われて設定の構築は成功する", func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("PORT", "9999")

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, "9999", cfg.Port)
		})

		t.Run("数値として解決される項目に、整数として解釈できない文字列が設定されているとき、設定の構築はエラーになる", func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("MATCHMAKING_TIMEOUT_SEC", "abc")

			_, err := FromEnv()

			require.Error(t, err)
		})

		t.Run("数値として解釈できる文字列が設定されているとき、その数値が使われる", func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("MATCHMAKING_TIMEOUT_SEC", "45")

			cfg, err := FromEnv()

			require.NoError(t, err)
			assert.Equal(t, 45, cfg.MatchmakingTimeoutSec)
		})

		t.Run(`真偽値として解決される項目に "true" でも "false" でもない文字列が設定されているとき、設定の構築はエラーになる`, func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("APP_FORCE_UPDATE", "yes")

			_, err := FromEnv()

			require.Error(t, err)
		})
	})

	t.Run("[設定]起動時設定の必須項目検証", func(t *testing.T) {
		t.Run("転送先となる各下流サービスの接続先URL(対戦・カード・マッチメイキング・アカウント・ショップ・シナリオ・ニュース・サポートの8種)のいずれか1つでも設定されていないとき、設定の構築はエラーになる", func(t *testing.T) {
			setValidRequiredEnvExcept(t, "BATTLE_SERVER_URL")

			_, err := FromEnv()

			require.Error(t, err)
		})

		t.Run("プレイヤー情報を保持するデータベースへの接続文字列が設定されていないとき、設定の構築はエラーになる", func(t *testing.T) {
			setValidRequiredEnvExcept(t, "DATABASE_CONN")

			_, err := FromEnv()

			require.Error(t, err)
		})

		t.Run("内部認証トークンの署名鍵が設定されていないとき、設定の構築はエラーになる", func(t *testing.T) {
			setValidRequiredEnvExcept(t, "INTERNAL_AUTH_PRIVATE_KEY")

			_, err := FromEnv()

			require.Error(t, err)
		})

		t.Run("転送先となる各下流サービスの接続先URL・プレイヤー情報を保持するデータベースへの接続文字列・内部認証トークンの署名鍵が全て設定されているとき、設定の構築は成功する", func(t *testing.T) {
			setValidRequiredEnv(t)

			cfg, err := FromEnv()

			require.NoError(t, err)
			require.NotNil(t, cfg)
		})

		t.Run("必須項目が空文字列として設定されているときも、未設定の場合と同様にエラーになる(この必須項目群では空文字列と未設定を区別しない)", func(t *testing.T) {
			setValidRequiredEnv(t)
			t.Setenv("BATTLE_SERVER_URL", "")

			_, err := FromEnv()

			require.Error(t, err)
		})
	})
}

func TestParseBool(t *testing.T) {
	t.Run("[設定]真偽値文字列の解釈", func(t *testing.T) {
		t.Run(`値が "true" のとき、真になる`, func(t *testing.T) {
			result, err := ParseBool("APP_FORCE_UPDATE", "true")

			require.NoError(t, err)
			assert.True(t, result)
		})

		t.Run(`値が "false" のとき、偽になる`, func(t *testing.T) {
			result, err := ParseBool("APP_FORCE_UPDATE", "false")

			require.NoError(t, err)
			assert.False(t, result)
		})

		t.Run(`値が "true"/"false" のどちらとも一致しないとき、エラーになる`, func(t *testing.T) {
			tests := []struct {
				name  string
				value string
			}{
				{name: "値が大文字を含む表記のとき、エラーになる", value: "True"},
				{name: `値が "1" のような別表記のとき、エラーになる`, value: "1"},
				{name: "値が空文字列のとき、エラーになる", value: ""},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					_, err := ParseBool("APP_FORCE_UPDATE", tt.value)

					require.Error(t, err)
				})
			}
		})
	})
}

func TestSplitCSV(t *testing.T) {
	t.Run("[設定]カンマ区切り環境変数のリスト化", func(t *testing.T) {
		t.Run("値が空文字列のとき、要素数0件のリストになる", func(t *testing.T) {
			result := splitCSV("")

			assert.Empty(t, result)
		})

		t.Run("値がカンマを含まない1つの文字列のとき、要素数1件のリストになる", func(t *testing.T) {
			result := splitCSV("foo")

			assert.Equal(t, []string{"foo"}, result)
		})

		t.Run("値がカンマ区切りの複数の文字列のとき、要素数が複数件のリストになる", func(t *testing.T) {
			result := splitCSV("foo,bar,baz")

			assert.Equal(t, []string{"foo", "bar", "baz"}, result)
		})

		t.Run("各要素の前後に空白が含まれるとき、空白が取り除かれた値になる", func(t *testing.T) {
			result := splitCSV(" foo , bar ")

			assert.Equal(t, []string{"foo", "bar"}, result)
		})

		t.Run("連続するカンマや空白のみの要素が含まれるとき、その要素はリストから除外される", func(t *testing.T) {
			result := splitCSV("foo,,bar,   ,baz")

			assert.Equal(t, []string{"foo", "bar", "baz"}, result)
		})
	})
}
