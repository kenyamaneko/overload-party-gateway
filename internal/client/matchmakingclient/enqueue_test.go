package matchmakingclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apimatchmaking "github.com/kenyamaneko/overload-party-matchmaking/packages/api-matchmaking"
)

func TestClient_SendsGatewayInstanceID(t *testing.T) {
	t.Run("gateway インスタンス識別子の送信", func(t *testing.T) {
		t.Run("生成時に渡した識別子が gateway_instance_id として送られる", func(t *testing.T) {
			const wantInstanceID = "test-instance-id"
			srv, received := newEnqueueRecorder(t)
			defer srv.Close()

			c := New(srv.URL, wantInstanceID, &http.Client{})
			require.NoError(t, c.Enqueue(context.Background(), 42, "alice", 7))

			require.Len(t, *received, 1)
			assert.Equal(t, wantInstanceID, (*received)[0].GatewayInstanceID)
		})

		t.Run("同じクライアントで続けて登録しても、gateway_instance_id は変わらない", func(t *testing.T) {
			srv, received := newEnqueueRecorder(t)
			defer srv.Close()

			c := New(srv.URL, "test-instance-id", &http.Client{})
			require.NoError(t, c.Enqueue(context.Background(), 42, "alice", 7))
			require.NoError(t, c.Enqueue(context.Background(), 43, "bob", 3))

			require.Len(t, *received, 2)
			assert.Equal(t, (*received)[0].GatewayInstanceID, (*received)[1].GatewayInstanceID)
		})
	})
}

// newEnqueueRecorder は Enqueue リクエストの body を受信順に記録するサーバを返す。
func newEnqueueRecorder(t *testing.T) (*httptest.Server, *[]apimatchmaking.EnqueueRequest) {
	t.Helper()
	received := make([]apimatchmaking.EnqueueRequest, 0, 2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req apimatchmaking.EnqueueRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		received = append(received, req)
		w.WriteHeader(http.StatusAccepted)
	}))
	return srv, &received
}
