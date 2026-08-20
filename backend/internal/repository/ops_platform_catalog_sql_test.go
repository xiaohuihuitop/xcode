package repository

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpsQueriesUsePlatformAccountAdapterColumn(t *testing.T) {
	for _, filename := range []string{
		"ops_repo_dashboard.go",
		"ops_repo_preagg.go",
		"ops_repo_request_details.go",
		"ops_repo_trends.go",
	} {
		t.Run(filename, func(t *testing.T) {
			source, err := os.ReadFile(filename)
			require.NoError(t, err)

			sql := strings.ToLower(string(source))
			require.NotContains(t, sql, "g.platform")
			require.Contains(t, sql, "g.account_platform")
		})
	}
}
