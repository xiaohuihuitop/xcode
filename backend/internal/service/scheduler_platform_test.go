//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSchedulerBucketRoundTripUsesPlatformID(t *testing.T) {
	bucket := SchedulerBucket{PlatformID: 17, Platform: PlatformOpenAI, Mode: SchedulerModeSingle}
	parsed, ok := ParseSchedulerBucket(bucket.String())
	require.True(t, ok)
	require.Equal(t, bucket, parsed)
}

func TestPlatformSchedulingScopeRejectsCrossPoolAccount(t *testing.T) {
	platformID := int64(8)
	scope := PlatformSchedulingScope{PlatformID: platformID, AccountPlatform: PlatformOpenAI}
	require.True(t, platformSchedulingScopeMatchesAccount(scope, &Account{PlatformID: &platformID, Platform: PlatformOpenAI}))
	otherID := int64(9)
	require.False(t, platformSchedulingScopeMatchesAccount(scope, &Account{PlatformID: &otherID, Platform: PlatformOpenAI}))
	require.False(t, platformSchedulingScopeMatchesAccount(scope, &Account{PlatformID: &platformID, Platform: PlatformGemini}))
}
