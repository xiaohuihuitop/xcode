//go:build unit

package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountFromServiceShallowIncludesPlatformPoolID(t *testing.T) {
	platformID := int64(42)
	account := AccountFromServiceShallow(&service.Account{
		ID:         7,
		Name:       "gpt-primary",
		Platform:   service.PlatformOpenAI,
		PlatformID: &platformID,
		Type:       service.AccountTypeAPIKey,
	})

	raw, err := json.Marshal(account)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"platform_id":42`)
}
