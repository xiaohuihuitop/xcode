package gatewayruntime

import "context"

type GatewayRuntime interface {
	Dispatch(context.Context, Request, UsageSink) (Result, error)
}

type TokenCounter interface {
	CountTokens(context.Context, Request) (TokenCountResult, error)
}

type AccountRuntime interface {
	ProbeAccount(context.Context, AccountProbeRequest) (AccountProbeResult, error)
}

type PricingEngine interface {
	Quote(context.Context, PricingRequest) (PricingQuote, error)
}

type UsageSink interface {
	RecordFinal(context.Context, UsageEvent) error
}
