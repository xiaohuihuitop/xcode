//go:build integration

package repository

import (
	"context"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func createSubscriptionPlanForTest(t *testing.T, client *dbent.Client, name string) int64 {
	t.Helper()
	plan, err := client.SubscriptionPlan.Create().
		SetName(uniqueTestValue(t, name)).
		SetPrice(10).
		SetValidityDays(30).
		SetRateMultiplier(0.8).
		Save(context.Background())
	require.NoError(t, err)
	return plan.ID
}

func TestUserSubscriptionRepository_PersistsPlanSnapshot(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := NewUserSubscriptionRepository(client)
	user := mustCreateUser(t, client, &service.User{Email: uniqueTestValue(t, "subscription-user") + "@example.com"})
	admin := mustCreateUser(t, client, &service.User{Email: uniqueTestValue(t, "subscription-admin") + "@example.com", Role: service.RoleAdmin})
	planID := createSubscriptionPlanForTest(t, client, "snapshot-plan")
	daily, weekly, monthly := 5.0, 20.0, 50.0

	sub := &service.UserSubscription{
		UserID:                  user.ID,
		SubscriptionPlanID:      &planID,
		PlanNameSnapshot:        "snapshot-plan",
		DailyLimitUSDSnapshot:   &daily,
		WeeklyLimitUSDSnapshot:  &weekly,
		MonthlyLimitUSDSnapshot: &monthly,
		RateMultiplierSnapshot:  0.8,
		StartsAt:                time.Now().Add(-time.Hour),
		ExpiresAt:               time.Now().Add(24 * time.Hour),
		Status:                  service.SubscriptionStatusActive,
		AssignedBy:              &admin.ID,
		AssignedAt:              time.Now(),
	}
	require.NoError(t, repo.Create(ctx, sub))

	got, err := repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.Equal(t, planID, *got.SubscriptionPlanID)
	require.Equal(t, "snapshot-plan", got.PlanNameSnapshot)
	require.InDelta(t, 0.8, got.RateMultiplierSnapshot, 1e-9)
	require.InDelta(t, daily, *got.DailyLimitUSDSnapshot, 1e-9)
	require.NotNil(t, got.User)
	require.NotNil(t, got.AssignedByUser)
}

func TestUserSubscriptionRepository_ListsAuthorizedActivePlansInExpiryOrder(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := NewUserSubscriptionRepository(client).(*userSubscriptionRepository)
	user := mustCreateUser(t, client, &service.User{Email: uniqueTestValue(t, "active-plan-user") + "@example.com"})
	planA := createSubscriptionPlanForTest(t, client, "plan-a")
	planB := createSubscriptionPlanForTest(t, client, "plan-b")
	now := time.Now()

	first := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, SubscriptionPlanID: &planB, PlanNameSnapshot: "B", RateMultiplierSnapshot: 1, ExpiresAt: now.Add(2 * time.Hour)})
	second := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, SubscriptionPlanID: &planA, PlanNameSnapshot: "A", RateMultiplierSnapshot: 1, ExpiresAt: now.Add(4 * time.Hour)})
	mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, SubscriptionPlanID: &planA, PlanNameSnapshot: "expired", RateMultiplierSnapshot: 1, ExpiresAt: now.Add(-time.Hour)})

	got, err := repo.ListActiveByUserIDAndPlanIDs(ctx, user.ID, []int64{planA, planB})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, first.ID, got[0].ID)
	require.Equal(t, second.ID, got[1].ID)

	empty, err := repo.ListActiveByUserIDAndPlanIDs(ctx, user.ID, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestUserSubscriptionRepository_UsageWindowsAndSoftDelete(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := NewUserSubscriptionRepository(client)
	user := mustCreateUser(t, client, &service.User{Email: uniqueTestValue(t, "usage-window-user") + "@example.com"})
	planID := createSubscriptionPlanForTest(t, client, "usage-plan")
	sub := mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, SubscriptionPlanID: &planID, PlanNameSnapshot: "usage", RateMultiplierSnapshot: 1})
	windowStart := time.Now().Add(-time.Hour).Truncate(time.Second)

	require.NoError(t, repo.ActivateWindows(ctx, sub.ID, windowStart, windowStart))
	require.NoError(t, repo.IncrementUsage(ctx, sub.ID, 1.25))
	got, err := repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.InDelta(t, 1.25, got.DailyUsageUSD, 1e-9)
	require.InDelta(t, 1.25, got.WeeklyUsageUSD, 1e-9)
	require.InDelta(t, 1.25, got.MonthlyUsageUSD, 1e-9)

	newWindow := time.Now().Truncate(time.Second)
	require.NoError(t, repo.ResetDailyUsage(ctx, sub.ID, &windowStart, newWindow))
	got, err = repo.GetByID(ctx, sub.ID)
	require.NoError(t, err)
	require.Zero(t, got.DailyUsageUSD)
	require.InDelta(t, 1.25, got.WeeklyUsageUSD, 1e-9)

	require.NoError(t, repo.Delete(ctx, sub.ID))
	require.ErrorIs(t, repo.IncrementUsage(ctx, sub.ID, 1), service.ErrSubscriptionNotFound)
	deleted, err := repo.GetByIDIncludeDeleted(ctx, sub.ID)
	require.NoError(t, err)
	require.NotNil(t, deleted.DeletedAt)
	restored, err := repo.Restore(ctx, sub.ID, service.SubscriptionStatusActive)
	require.NoError(t, err)
	require.Nil(t, restored.DeletedAt)
}

func TestUserSubscriptionRepository_ListAndExpire(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	client := tx.Client()
	repo := NewUserSubscriptionRepository(client)
	user := mustCreateUser(t, client, &service.User{Email: uniqueTestValue(t, "list-subscription-user") + "@example.com"})
	planID := createSubscriptionPlanForTest(t, client, "list-plan")
	mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, SubscriptionPlanID: &planID, PlanNameSnapshot: "active", RateMultiplierSnapshot: 1, ExpiresAt: time.Now().Add(time.Hour)})
	mustCreateSubscription(t, client, &service.UserSubscription{UserID: user.ID, SubscriptionPlanID: &planID, PlanNameSnapshot: "expired", RateMultiplierSnapshot: 1, ExpiresAt: time.Now().Add(-time.Hour)})

	active, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, &user.ID, service.SubscriptionStatusActive, "expires_at", "asc")
	require.NoError(t, err)
	require.Len(t, active, 1)
	require.Equal(t, int64(1), page.Total)

	updated, err := repo.BatchUpdateExpiredStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), updated)
	expired, page, err := repo.List(ctx, pagination.PaginationParams{Page: 1, PageSize: 10}, &user.ID, service.SubscriptionStatusExpired, "expires_at", "asc")
	require.NoError(t, err)
	require.Len(t, expired, 1)
	require.Equal(t, int64(1), page.Total)
}

func TestUserSubscriptionRepository_RejectsNilInput(t *testing.T) {
	repo := NewUserSubscriptionRepository(testEntTx(t).Client())
	require.ErrorIs(t, repo.Create(context.Background(), nil), service.ErrSubscriptionNilInput)
	require.ErrorIs(t, repo.Update(context.Background(), nil), service.ErrSubscriptionNilInput)
}
