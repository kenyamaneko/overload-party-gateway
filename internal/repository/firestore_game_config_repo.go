package repository

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

const gameConfigCollection = "game_config"

var _ port.GameConfigRepo = (*FirestoreGameConfigRepository)(nil)

// FirestoreGameConfigRepository は Firestore を使用した GameConfigRepo 実装。
type FirestoreGameConfigRepository struct {
	client *firestore.Client
}

func NewFirestoreGameConfigRepository(client *firestore.Client) *FirestoreGameConfigRepository {
	return &FirestoreGameConfigRepository{client: client}
}

// GetInt64 は指定キーの設定値を int64 で返します。
// 設定が保存されていない場合は port.ErrNotFound を、設定は保存されているが値が未設定の場合は
// それとは別のエラーを返します（いずれも fail-fast）。
func (r *FirestoreGameConfigRepository) GetInt64(ctx context.Context, key string) (int64, error) {
	snap, err := r.client.Collection(gameConfigCollection).Doc(key).Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return 0, fmt.Errorf("game config %q: %w", key, port.ErrNotFound)
		}
		return 0, fmt.Errorf("get game config %q: %w", key, err)
	}

	if _, ok := snap.Data()["value"]; !ok {
		return 0, fmt.Errorf("game config %q: value field missing", key)
	}

	var doc struct {
		Value int64 `firestore:"value"`
	}
	if err := snap.DataTo(&doc); err != nil {
		return 0, fmt.Errorf("parse game config %q: %w", key, err)
	}
	return doc.Value, nil
}
