package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

// PlatformSchedulingScope identifies one V2 provider account pool. AccountPlatform
// is the internal protocol adapter, while PlatformID is the business-pool boundary.
type PlatformSchedulingScope struct {
	PlatformID      int64
	PlatformCode    string
	AccountPlatform string
}

// PlatformPoolAccountLister stays deliberately narrow so legacy repository test
// doubles do not need to implement V2-only scheduling methods.
type PlatformPoolAccountLister interface {
	ListSchedulableByPlatformPool(ctx context.Context, platformID int64, accountPlatform string) ([]Account, error)
}

type PlatformAccountBindingValidator interface {
	ValidatePlatformAccountBinding(ctx context.Context, platformID int64, accountPlatform string) error
}

// PlatformAccountBindingResolver returns the server-owned adapter snapshot for
// one platform pool. Account create/update must derive Platform from this
// record rather than trusting a client-supplied adapter string.
type PlatformAccountBinding struct {
	ID              int64
	Code            string
	AccountPlatform string
	Status          string
}

type PlatformAccountBindingResolver interface {
	ResolvePlatformAccountBinding(ctx context.Context, platformID int64) (PlatformAccountBinding, error)
}

func WithPlatformSchedulingScope(ctx context.Context, scope PlatformSchedulingScope) context.Context {
	normalized, ok := normalizePlatformSchedulingScope(scope)
	if ctx == nil || !ok {
		return ctx
	}
	return context.WithValue(ctx, ctxkey.PlatformSchedulingScope, normalized)
}

func PlatformSchedulingScopeFromContext(ctx context.Context) (PlatformSchedulingScope, bool) {
	if ctx == nil {
		return PlatformSchedulingScope{}, false
	}
	scope, ok := ctx.Value(ctxkey.PlatformSchedulingScope).(PlatformSchedulingScope)
	if !ok {
		return PlatformSchedulingScope{}, false
	}
	return normalizePlatformSchedulingScope(scope)
}

func listPlatformPoolSchedulableAccounts(
	ctx context.Context,
	lister PlatformPoolAccountLister,
	scope PlatformSchedulingScope,
) ([]Account, error) {
	normalized, ok := normalizePlatformSchedulingScope(scope)
	if !ok {
		return nil, fmt.Errorf("%w: invalid platform scheduling scope", ErrPlatformInvalid)
	}
	if lister == nil {
		return nil, fmt.Errorf("%w: platform account pool is not configured", ErrPlatformInvalid)
	}
	return lister.ListSchedulableByPlatformPool(ctx, normalized.PlatformID, normalized.AccountPlatform)
}

func normalizePlatformSchedulingScope(scope PlatformSchedulingScope) (PlatformSchedulingScope, bool) {
	scope.PlatformCode = strings.ToLower(strings.TrimSpace(scope.PlatformCode))
	scope.AccountPlatform = strings.ToLower(strings.TrimSpace(scope.AccountPlatform))
	return scope, scope.PlatformID > 0 && scope.AccountPlatform != ""
}

// platformSchedulingCacheID maps a Platform to the existing numeric cache
// namespace used by sticky sessions. Negative IDs cannot collide with accounts.
func platformSchedulingCacheID(scope PlatformSchedulingScope) *int64 {
	normalized, ok := normalizePlatformSchedulingScope(scope)
	if !ok {
		return nil
	}
	cacheID := -normalized.PlatformID - 1
	return &cacheID
}

// PlatformSchedulingID returns the internal cache namespace for the Platform
// selected for the current request. It is not a billing or persistence ID.
func PlatformSchedulingID(ctx context.Context) *int64 {
	scope, ok := PlatformSchedulingScopeFromContext(ctx)
	if !ok {
		return nil
	}
	return platformSchedulingCacheID(scope)
}

// PlatformAssetID returns the persistent business Platform primary key.
// Unlike PlatformSchedulingID, this value is safe for usage, audit, and
// identity records and must never be used as a cache namespace.
func PlatformAssetID(ctx context.Context) *int64 {
	scope, ok := PlatformSchedulingScopeFromContext(ctx)
	if !ok {
		return nil
	}
	platformID := scope.PlatformID
	return &platformID
}

func accountMatchesPlatformSchedulingScope(ctx context.Context, account *Account) bool {
	scope, scoped := PlatformSchedulingScopeFromContext(ctx)
	return !scoped || platformSchedulingScopeMatchesAccount(scope, account)
}

func platformSchedulingScopeMatchesAccount(scope PlatformSchedulingScope, account *Account) bool {
	normalized, ok := normalizePlatformSchedulingScope(scope)
	if !ok || account == nil || account.PlatformID == nil {
		return false
	}
	return *account.PlatformID == normalized.PlatformID &&
		strings.EqualFold(strings.TrimSpace(account.Platform), normalized.AccountPlatform)
}
