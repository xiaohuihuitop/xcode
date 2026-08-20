package domain

// ModelPricingInterval is a user-configured pricing tier. Prices are stored
// as pointers so an explicit zero remains distinguishable from an omitted
// field and can intentionally disable a LiteLLM price component.
type ModelPricingInterval struct {
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       *int     `json:"max_tokens,omitempty"`
	TierLabel       string   `json:"tier_label,omitempty"`
	InputPrice      *float64 `json:"input_price,omitempty"`
	OutputPrice     *float64 `json:"output_price,omitempty"`
	CacheWritePrice *float64 `json:"cache_write_price,omitempty"`
	CacheReadPrice  *float64 `json:"cache_read_price,omitempty"`
	PerRequestPrice *float64 `json:"per_request_price,omitempty"`
	SortOrder       int      `json:"sort_order,omitempty"`
}
