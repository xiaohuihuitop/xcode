package productcore

import "context"

type PlatformCatalog interface {
	ListModelCandidates(context.Context, string) ([]*Platform, error)
}

type AssetSelector interface {
	Select(context.Context, AccessGrant, bool) (*BillingAsset, error)
}
