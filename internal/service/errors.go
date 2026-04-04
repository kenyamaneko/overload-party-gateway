package service

import (
	"errors"

	"github.com/kenyamaneko/overload-party-gateway/internal/port"
)

var (
	ErrNotFound                  = port.ErrNotFound
	ErrFactionAlreadySelected    = errors.New("faction already selected")
	ErrInvalidFaction            = errors.New("invalid faction")
	ErrReceiptVerificationFailed = errors.New("receipt verification failed")
	ErrSubVerificationFailed     = errors.New("subscription verification failed")
	ErrPlayerAlreadyRegistered   = errors.New("player already registered")
	ErrPlayerNotFound            = errors.New("player not found")
	ErrUnsupportedPlatform       = errors.New("unsupported platform")
	ErrProductNotActive          = errors.New("product is not active")
	ErrProductNotSubscription    = errors.New("product is not a subscription")
	ErrAlreadyOwned              = errors.New("product already owned")
)
