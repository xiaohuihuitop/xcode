package productcore

type AccessGrant struct {
	KeyID               int64
	UserID              int64
	Balance             float64
	PlatformIDs         []int64
	SubscriptionPlanIDs []int64
	AllowBalance        bool
}

type Request struct {
	Model              string
	EndpointCapability string
	SkipBilling        bool
}

type Platform struct {
	ID                   int64
	Code                 string
	AccountPlatform      string
	RequestedModel       string
	UpstreamModel        string
	EndpointCapabilities []string
	MatchPriority        int
}

type BillingAsset struct {
	Source         string
	SubscriptionID *int64
	PlanID         *int64
	RateMultiplier float64
}

type Decision struct {
	Platform     Platform
	BillingAsset *BillingAsset
}
