package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiaccount "github.com/kenyamaneko/overload-party-account/packages/api-account"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newEmulatedFirebaseAuthClient は Firebase Admin SDK の Auth エミュレーターモード
// (FIREBASE_AUTH_EMULATOR_HOST) を使い、実クレデンシャル無しで動く *auth.Client を返す。
// エミュレーターモードでは ID トークンの署名検証はスキップされるが、失効確認のために
// 内部で GetUser 相当の HTTP 呼び出しが飛ぶため、それに応答する最小限のダミーサーバーを立てる。
func newEmulatedFirebaseAuthClient(t *testing.T, projectID string) *auth.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/identitytoolkit.googleapis.com/v1/projects/"+projectID+"/accounts:lookup", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"users":[{"localId":"emulated-lookup"}]}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	host := strings.TrimPrefix(server.URL, "http://")
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", host)

	app, err := firebase.NewApp(context.Background(), &firebase.Config{ProjectID: projectID})
	require.NoError(t, err)
	client, err := app.Auth(context.Background())
	require.NoError(t, err)
	return client
}

func mintEmulatedIDToken(t *testing.T, projectID, uid string) string {
	t.Helper()
	now := time.Now()
	header := map[string]any{"alg": "none", "typ": "JWT"}
	payload := map[string]any{
		"iss": "https://securetoken.google.com/" + projectID,
		"aud": projectID,
		"sub": uid,
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
	return b64json(t, header) + "." + b64json(t, payload) + ".sig"
}

func b64json(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(data)
}

func newHandlerTestServer(t *testing.T, h *Handler) string {
	t.Helper()
	r := gin.New()
	r.GET("/ws", h.HandleUpgrade)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	return server.URL
}

func dialUpgrade(t *testing.T, serverURL, token string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws?token=" + url.QueryEscape(token)
	return websocket.DefaultDialer.Dial(wsURL, header)
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(data)
}

func assertUpgradeRejected(t *testing.T, serverURL, token string, header http.Header, wantStatus int, wantBodyContains string) {
	t.Helper()
	conn, resp, err := dialUpgrade(t, serverURL, token, header)
	require.Error(t, err)
	require.NotNil(t, resp)
	if conn != nil {
		_ = conn.Close()
	}
	assert.Equal(t, wantStatus, resp.StatusCode)
	assert.Contains(t, readBody(t, resp), wantBodyContains)
}

func assertUpgradeEstablished(t *testing.T, serverURL, token string, header http.Header) {
	t.Helper()
	conn, resp, err := dialUpgrade(t, serverURL, token, header)
	if err != nil {
		t.Fatalf("expected the connection to upgrade, got status=%d body=%q err=%v", resp.StatusCode, readBody(t, resp), err)
	}
	_ = conn.Close()
}

func registeredAccountStub(playerID string) *stubAccountClient {
	return &stubAccountClient{findByFirebaseUIDFunc: func(_ context.Context, firebaseUID string) (*apiaccount.PlayerResponse, error) {
		return &apiaccount.PlayerResponse{PlayerID: playerID, FirebaseUID: firebaseUID}, nil
	}}
}

func unregisteredAccountStub() *stubAccountClient {
	return &stubAccountClient{findByFirebaseUIDFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
		return nil, port.ErrAccountNotFound
	}}
}

// playerNotFoundAccountStub は、照会自体はエラーにならないがプレイヤーが見つからない
// (nil が返る) 状態を表すスタブである。
func playerNotFoundAccountStub() *stubAccountClient {
	return &stubAccountClient{findByFirebaseUIDFunc: func(_ context.Context, _ string) (*apiaccount.PlayerResponse, error) {
		return nil, nil
	}}
}

func TestHandleUpgrade(t *testing.T) {
	t.Run("[認証]接続要求の認証判定", func(t *testing.T) {
		t.Run("接続要求にトークンが含まれていない(空文字)とき、本番運用・開発運用のいずれでも、HTTPステータス401、エラー内容\"missing token\"が返り、接続はWebSocketへ確立されない", func(t *testing.T) {
			t.Run("開発運用のとき", func(t *testing.T) {
				h := NewHandler(newTestManager(t, managerDeps{}), nil, &stubAccountClient{}, nil)
				serverURL := newHandlerTestServer(t, h)

				assertUpgradeRejected(t, serverURL, "", nil, http.StatusUnauthorized, "missing token")
			})

			t.Run("本番運用のとき", func(t *testing.T) {
				authClient := newEmulatedFirebaseAuthClient(t, "project-missing-token")
				h := NewHandler(newTestManager(t, managerDeps{}), authClient, &stubAccountClient{}, nil)
				serverURL := newHandlerTestServer(t, h)

				assertUpgradeRejected(t, serverURL, "", nil, http.StatusUnauthorized, "missing token")
			})
		})

		t.Run("トークンがFirebase IDトークンとして無効なとき、HTTPステータス401、エラー内容\"invalid token\"が返り、接続は確立されない", func(t *testing.T) {
			authClient := newEmulatedFirebaseAuthClient(t, "project-invalid-token")
			h := NewHandler(newTestManager(t, managerDeps{}), authClient, &stubAccountClient{}, nil)
			serverURL := newHandlerTestServer(t, h)

			assertUpgradeRejected(t, serverURL, "not-a-valid-jwt", nil, http.StatusUnauthorized, "invalid token")
		})

		t.Run("トークンは有効だが対応するプレイヤーが登録されていないとき、HTTPステータス401、エラー内容\"player not registered\"が返り、接続は確立されない", func(t *testing.T) {
			t.Run("プレイヤーの照会自体が失敗するとき", func(t *testing.T) {
				authClient := newEmulatedFirebaseAuthClient(t, "project-unregistered")
				account := unregisteredAccountStub()
				h := NewHandler(newTestManager(t, managerDeps{account: account}), authClient, account, nil)
				serverURL := newHandlerTestServer(t, h)
				token := mintEmulatedIDToken(t, "project-unregistered", "uid-unregistered")

				assertUpgradeRejected(t, serverURL, token, nil, http.StatusUnauthorized, "player not registered")
			})

			t.Run("プレイヤーの照会はエラーにならないが、対象のプレイヤーが見つからないとき", func(t *testing.T) {
				authClient := newEmulatedFirebaseAuthClient(t, "project-not-found")
				account := playerNotFoundAccountStub()
				h := NewHandler(newTestManager(t, managerDeps{account: account}), authClient, account, nil)
				serverURL := newHandlerTestServer(t, h)
				token := mintEmulatedIDToken(t, "project-not-found", "uid-not-found")

				assertUpgradeRejected(t, serverURL, token, nil, http.StatusUnauthorized, "player not registered")
			})
		})

		t.Run("トークンが有効で対応するプレイヤーが登録されているとき、そのプレイヤーとして接続がWebSocketに確立される", func(t *testing.T) {
			authClient := newEmulatedFirebaseAuthClient(t, "project-registered")
			account := registeredAccountStub("player-registered")
			h := NewHandler(newTestManager(t, managerDeps{account: account}), authClient, account, nil)
			serverURL := newHandlerTestServer(t, h)
			token := mintEmulatedIDToken(t, "project-registered", "uid-registered")

			assertUpgradeEstablished(t, serverURL, token, nil)
		})

		t.Run("トークンが開発用トークンの形式でないとき、HTTPステータス401、エラー内容\"invalid dev token format\"が返り、接続は確立されない", func(t *testing.T) {
			h := NewHandler(newTestManager(t, managerDeps{}), nil, &stubAccountClient{}, nil)
			serverURL := newHandlerTestServer(t, h)

			assertUpgradeRejected(t, serverURL, "not-a-dev-token", nil, http.StatusUnauthorized, "invalid dev token format")
		})

		t.Run("トークンが開発用トークンの形式だが対応するプレイヤーが登録されていないとき、HTTPステータス401、エラー内容\"player not registered\"が返り、接続は確立されない", func(t *testing.T) {
			t.Run("プレイヤーの照会自体が失敗するとき", func(t *testing.T) {
				account := unregisteredAccountStub()
				h := NewHandler(newTestManager(t, managerDeps{account: account}), nil, account, nil)
				serverURL := newHandlerTestServer(t, h)

				assertUpgradeRejected(t, serverURL, "dev-token-uid-unregistered", nil, http.StatusUnauthorized, "player not registered")
			})

			t.Run("プレイヤーの照会はエラーにならないが、対象のプレイヤーが見つからないとき", func(t *testing.T) {
				account := playerNotFoundAccountStub()
				h := NewHandler(newTestManager(t, managerDeps{account: account}), nil, account, nil)
				serverURL := newHandlerTestServer(t, h)

				assertUpgradeRejected(t, serverURL, "dev-token-uid-not-found", nil, http.StatusUnauthorized, "player not registered")
			})
		})

		t.Run("トークンが開発用トークンの形式で対応するプレイヤーが登録されているとき、そのプレイヤーとして接続がWebSocketに確立される", func(t *testing.T) {
			account := registeredAccountStub("player-dev-registered")
			h := NewHandler(newTestManager(t, managerDeps{account: account}), nil, account, nil)
			serverURL := newHandlerTestServer(t, h)

			assertUpgradeEstablished(t, serverURL, "dev-token-uid-registered", nil)
		})
	})

	t.Run("[接続管理]接続元オリジンによる許可判定", func(t *testing.T) {
		t.Run("許可オリジンの制限が1件も設定されていない(0件、制限なしモード)とき、どのOriginからの接続要求も許可される", func(t *testing.T) {
			account := registeredAccountStub("player-any-origin")
			h := NewHandler(newTestManager(t, managerDeps{account: account}), nil, account, nil)
			serverURL := newHandlerTestServer(t, h)

			assertUpgradeEstablished(t, serverURL, "dev-token-uid-any-origin", http.Header{"Origin": []string{"https://arbitrary.example"}})
		})

		t.Run("許可オリジンが1件以上設定されており、接続要求のOriginがその中に含まれるとき、接続が許可される", func(t *testing.T) {
			account := registeredAccountStub("player-allowed-origin")
			h := NewHandler(newTestManager(t, managerDeps{account: account}), nil, account, []string{"https://allowed.example"})
			serverURL := newHandlerTestServer(t, h)

			assertUpgradeEstablished(t, serverURL, "dev-token-uid-allowed-origin", http.Header{"Origin": []string{"https://allowed.example"}})
		})

		t.Run("許可オリジンが1件以上設定されており、接続要求のOriginがその中に含まれない(Originヘッダが送られてこない場合を含む)とき、接続は拒否される", func(t *testing.T) {
			cases := []struct {
				name   string
				header http.Header
			}{
				{"許可外のOriginを送ったとき", http.Header{"Origin": []string{"https://not-allowed.example"}}},
				{"Originヘッダを送らなかったとき", nil},
			}
			for _, tt := range cases {
				t.Run(tt.name, func(t *testing.T) {
					account := registeredAccountStub("player-rejected-origin")
					h := NewHandler(newTestManager(t, managerDeps{account: account}), nil, account, []string{"https://allowed.example"})
					serverURL := newHandlerTestServer(t, h)

					_, resp, err := dialUpgrade(t, serverURL, "dev-token-uid-rejected-origin", tt.header)

					require.Error(t, err)
					require.NotNil(t, resp)
					assert.NotEqual(t, http.StatusSwitchingProtocols, resp.StatusCode)
				})
			}
		})
	})
}
