package productcore

import "errors"

var (
	ErrModelUnavailable     = errors.New("product core model is unavailable")
	ErrPlatformForbidden    = errors.New("api key is not authorized for platform")
	ErrPlatformAmbiguous    = errors.New("model resolves to multiple equally preferred platforms")
	ErrEndpointUnsupported  = errors.New("platform does not support endpoint")
	ErrNoBillingAsset       = errors.New("no usable billing asset")
	ErrInsufficientBalance  = errors.New("insufficient balance")
	ErrDailyLimitExceeded   = errors.New("daily limit exceeded")
	ErrWeeklyLimitExceeded  = errors.New("weekly limit exceeded")
	ErrMonthlyLimitExceeded = errors.New("monthly limit exceeded")
)
