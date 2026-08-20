package repository

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageLogEffectivePlatformExprUsesPlatformCatalog(t *testing.T) {
	expr := strings.ToLower(usageLogEffectivePlatformExpr)

	require.Contains(t, expr, "p.code")
	require.Contains(t, expr, "coalesce")
	require.NotContains(t, expr, "g.platform")
	require.NotContains(t, expr, "a.platform")
}
