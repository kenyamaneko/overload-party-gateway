package rest

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kenyamaneko/overload-party-gateway/internal/service"
)

// errorStatus maps a service-layer error to an HTTP status code.
func errorStatus(err error) int {
	switch {
	case errors.Is(err, service.ErrNotFound),
		errors.Is(err, service.ErrPlayerNotFound):
		return http.StatusNotFound
	case errors.Is(err, service.ErrFactionAlreadySelected),
		errors.Is(err, service.ErrAlreadyOwned),
		errors.Is(err, service.ErrPlayerAlreadyRegistered):
		return http.StatusConflict
	case errors.Is(err, service.ErrInvalidFaction),
		errors.Is(err, service.ErrProductNotSubscription),
		errors.Is(err, service.ErrProductNotActive),
		errors.Is(err, service.ErrUnsupportedPlatform):
		return http.StatusBadRequest
	case errors.Is(err, service.ErrReceiptVerificationFailed),
		errors.Is(err, service.ErrSubVerificationFailed):
		return http.StatusPaymentRequired
	default:
		return http.StatusInternalServerError
	}
}

// respondError writes a JSON error response with the appropriate status code.
func respondError(c *gin.Context, err error) {
	c.JSON(errorStatus(err), gin.H{"error": err.Error()})
}
