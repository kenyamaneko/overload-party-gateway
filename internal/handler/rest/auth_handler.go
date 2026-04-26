package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

// AuthHandler は認証関連の REST エンドポイントを処理します
type AuthHandler struct {
	account *accountclient.Client
}

// NewAuthHandler は AuthHandler を生成します
func NewAuthHandler(account *accountclient.Client) *AuthHandler {
	return &AuthHandler{account: account}
}

// Register はプレイヤー新規登録を処理します。表示名は受け取らず、
// オンボーディングシナリオの中で player-onboarded イベント経由で確定します。
func (h *AuthHandler) Register(c *gin.Context) {
	firebaseUID := middleware.GetFirebaseUID(c)
	if firebaseUID == "" {
		// FirebaseAuth の後にチェインされる前提だが、防御的に 401 を返す。
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing firebase uid"})
		return
	}
	player, err := h.account.Register(c.Request.Context(), firebaseUID)
	if err != nil {
		if errors.Is(err, accountclient.ErrPlayerAlreadyRegistered) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, player)
}

// Login はプレイヤーログインを処理します
func (h *AuthHandler) Login(c *gin.Context) {
	firebaseUID := middleware.GetFirebaseUID(c)
	if firebaseUID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing firebase uid"})
		return
	}
	player, err := h.account.Login(c.Request.Context(), firebaseUID)
	if err != nil {
		if errors.Is(err, accountclient.ErrNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, player)
}
