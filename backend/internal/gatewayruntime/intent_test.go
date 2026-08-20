//go:build unit

package gatewayruntime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDispatchIntentContextRoundTripsIndependentCopies(t *testing.T) {
	intent := &DispatchIntent{
		Platform: PlatformRoute{ID: 3, EndpointCapabilities: []string{"responses"}},
	}
	ctx := WithDispatchIntent(context.Background(), intent)
	intent.Platform.EndpointCapabilities[0] = "mutated-before-read"

	first, ok := DispatchIntentFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "responses", first.Platform.EndpointCapabilities[0])

	first.Platform.EndpointCapabilities[0] = "mutated-after-read"
	second, ok := DispatchIntentFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, "responses", second.Platform.EndpointCapabilities[0])
}

func TestDispatchIntentCarriesOnlyRuntimeRoute(t *testing.T) {
	ctx := WithDispatchIntent(context.Background(), &DispatchIntent{
		Platform: PlatformRoute{ID: 3},
	})
	got, ok := DispatchIntentFromContext(ctx)
	require.True(t, ok)
	require.Equal(t, int64(3), got.Platform.ID)
}
