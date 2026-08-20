package service

// resolveBillingMultipliers returns the selected asset multiplier for every
// billing modality. Base model/media prices are resolved independently.
func resolveBillingMultipliers(
	subscription *UserSubscription,
	fallback float64,
) (token, image, video float64) {
	base := resolveBillingRateMultiplier(subscription, fallback)
	return base, base, base
}

func resolveBillingRateMultiplier(subscription *UserSubscription, fallback float64) float64 {
	if subscription != nil {
		return nonNegativeMultiplier(subscription.RateMultiplierSnapshot)
	}
	return nonNegativeMultiplier(fallback)
}

func nonNegativeMultiplier(multiplier float64) float64 {
	if multiplier < 0 {
		return 0
	}
	return multiplier
}
