package rest

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	apigateway "github.com/kenyamaneko/overload-party-gateway/packages/api-gateway"
	"github.com/kenyamaneko/overload-party-gateway/internal/client/accountclient"
	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
)

const maxNameRunes = 50

// validateName は gateway 段階で簡単に弾けるプレイヤー名の不正を検出する。
// 詳細な業務ルール（重複等）は account サービス側に委ねる。
func validateName(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("name must not be empty or whitespace only")
	}
	n := utf8.RuneCountInString(s)
	if n < 1 || n > maxNameRunes {
		return errors.New("name must be 1-50 characters")
	}
	for _, r := range s {
		// 制御文字（改行・タブ含む）は明らかに不正なので gateway で弾く。
		if unicode.IsControl(r) {
			return errors.New("name must not contain control characters")
		}
	}
	return nil
}

// AuthHandler は認証関連の REST エンドポイントを処理します
type AuthHandler struct {
	account *accountclient.Client
}

// NewAuthHandler は AuthHandler を生成します
func NewAuthHandler(account *accountclient.Client) *AuthHandler {
	return &AuthHandler{account: account}
}

// Register はプレイヤー新規登録を処理します
func (h *AuthHandler) Register(c *gin.Context) {
	var req apigateway.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateName(req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	firebaseUID := middleware.GetFirebaseUID(c)
	if firebaseUID == "" {
		// FirebaseAuth の後にチェインされる前提だが、防御的に 401 を返す。
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing firebase uid"})
		return
	}
	player, err := h.account.Register(c.Request.Context(), firebaseUID, req.Name)
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
