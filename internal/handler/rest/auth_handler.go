package rest

import (
	"net/http"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/middleware"
	"github.com/kenyamaneko/overload-party-gateway/internal/model"
	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

const maxUsernameRunes = 50

type AuthHandler struct {
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req model.RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	// Generated API types carry no validation tags; enforce length here to
	// preserve the original binding:"required,min=1,max=50" contract.
	if n := utf8.RuneCountInString(req.Username); n < 1 || n > maxUsernameRunes {
		c.JSON(http.StatusBadRequest, gin.H{"error": "username must be 1-50 characters"})
		return
	}

	firebaseUID := middleware.GetFirebaseUID(c)

	player, err := h.authService.Register(c.Request.Context(), firebaseUID, req.Username)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, player)
}

func (h *AuthHandler) Login(c *gin.Context) {
	firebaseUID := middleware.GetFirebaseUID(c)

	player, err := h.authService.Login(c.Request.Context(), firebaseUID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, player)
}
