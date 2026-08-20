package service

import (
	"context"
	"time"
)

const PlatformStatusActive = "active"

// Platform owns one provider-specific account pool.
type Platform struct {
	ID                   int64
	Code                 string
	Name                 string
	AccountPlatform      string
	Status               string
	EndpointCapabilities []string
	SchedulingConfig     map[string]any
	ModelRules           []PlatformModelRule
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

func (p Platform) IsActive() bool {
	return p.Status == "" || p.Status == PlatformStatusActive
}

// PlatformModelRule maps one requested model name or a suffix wildcard to a
// platform. The persisted status is adapted to Enabled at the repository edge.
type PlatformModelRule struct {
	ID                   int64
	PlatformID           int64
	PlatformCode         string
	AccountPlatform      string
	ModelPattern         string
	UpstreamModel        string
	EndpointCapabilities []string
	Enabled              bool
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// PlatformModelResolver returns all active platform candidates for a model.
// ProductCore applies API Key authorization and endpoint capability filtering
// before selecting the actual platform.
type PlatformModelResolver interface {
	ResolveModelCandidates(ctx context.Context, requestedModel string) ([]*ResolvedPlatformModel, error)
}

// ResolvedPlatformModel is one platform target for a client model.
type ResolvedPlatformModel struct {
	PlatformID           int64
	PlatformCode         string
	AccountPlatform      string
	RequestedModel       string
	UpstreamModel        string
	EndpointCapabilities []string
	MatchPriority        int
	RuleID               int64
}
