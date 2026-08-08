package rest

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

// AuthHandler は認証関連の REST エンドポイントを処理します
type AuthHandler struct {
	account port.AccountClient
}

// NewAuthHandler は AuthHandler を生成します
func NewAuthHandler(account port.AccountClient) *AuthHandler {
	return &AuthHandler{account: account}
}

// Register はプレイヤー新規登録を処理します。表示名は受け取らず、
// オンボーディングシナリオの中で確定します。
func (h *AuthHandler) Register(c *gin.Context) {
	firebaseUID := middleware.GetFirebaseUID(c)
	if firebaseUID == "" {
		// UseFirebaseAuth の後にチェインされる前提だが、防御的に 401 を返す。
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing firebase uid"})
		return
	}
	player, err := h.account.Register(c.Request.Context(), firebaseUID)
	if err != nil {
		if errors.Is(err, port.ErrPlayerAlreadyRegistered) {
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
		if errors.Is(err, port.ErrAccountNotFound) {
			// 初回ログイン時は未登録が正常系のため、404 だが Info として記録する。
			middleware.SetRequestLogLevel(c, slog.LevelInfo)
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, player)
}
