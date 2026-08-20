package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSparkShadowUsesCanonicalModelPredicate(t *testing.T) {
	pid := int64(1)
	shadow := &Account{ParentAccountID: &pid, QuotaDimension: QuotaDimensionSpark, Platform: PlatformOpenAI}
	require.True(t, shadow.IsModelSupported("gpt-5.3-codex-spark"))
	require.False(t, shadow.IsModelSupported("gpt-5.3-codex"))
}
