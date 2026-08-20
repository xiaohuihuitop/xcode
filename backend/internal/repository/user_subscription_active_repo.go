package repository

import (
	"context"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/usersubscription"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *userSubscriptionRepository) ListActiveByUserIDAndPlanIDs(
	ctx context.Context,
	userID int64,
	planIDs []int64,
) ([]service.UserSubscription, error) {
	if len(planIDs) == 0 {
		return []service.UserSubscription{}, nil
	}
	client := clientFromContext(ctx, r.client)
	entities, err := client.UserSubscription.Query().
		Where(
			usersubscription.UserIDEQ(userID),
			usersubscription.StatusEQ(service.SubscriptionStatusActive),
			usersubscription.ExpiresAtGT(time.Now()),
			usersubscription.SubscriptionPlanIDIn(planIDs...),
		).
		Order(
			dbent.Asc(usersubscription.FieldExpiresAt),
			dbent.Asc(usersubscription.FieldCreatedAt),
			dbent.Asc(usersubscription.FieldID),
		).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return userSubscriptionEntitiesToService(entities), nil
}
