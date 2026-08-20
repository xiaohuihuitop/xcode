//go:build unit

package service

import (
	"context"
	"sort"
)

type userSubRepoNoop struct {
	UserSubscriptionRepository
}

type subscriptionUserSubRepoStub struct {
	userSubRepoNoop
	nextID      int64
	byID        map[int64]*UserSubscription
	createCalls int
}

func newSubscriptionUserSubRepoStub() *subscriptionUserSubRepoStub {
	return &subscriptionUserSubRepoStub{
		nextID: 1,
		byID:   make(map[int64]*UserSubscription),
	}
}

func (s *subscriptionUserSubRepoStub) seed(subscription *UserSubscription) {
	if subscription == nil {
		return
	}
	copy := *subscription
	if copy.ID == 0 {
		copy.ID = s.nextID
		s.nextID++
	}
	s.byID[copy.ID] = &copy
}

func (s *subscriptionUserSubRepoStub) Create(_ context.Context, subscription *UserSubscription) error {
	if subscription == nil {
		return nil
	}
	s.createCalls++
	copy := *subscription
	if copy.ID == 0 {
		copy.ID = s.nextID
		s.nextID++
	}
	subscription.ID = copy.ID
	s.byID[copy.ID] = &copy
	return nil
}

func (s *subscriptionUserSubRepoStub) GetByID(_ context.Context, id int64) (*UserSubscription, error) {
	subscription := s.byID[id]
	if subscription == nil {
		return nil, ErrSubscriptionNotFound
	}
	copy := *subscription
	return &copy, nil
}

func (s *subscriptionUserSubRepoStub) ListByUserID(_ context.Context, userID int64) ([]UserSubscription, error) {
	subscriptions := make([]UserSubscription, 0, len(s.byID))
	for _, subscription := range s.byID {
		if subscription != nil && subscription.UserID == userID {
			subscriptions = append(subscriptions, *subscription)
		}
	}
	sort.Slice(subscriptions, func(i, j int) bool { return subscriptions[i].ID < subscriptions[j].ID })
	return subscriptions, nil
}

func (s *subscriptionUserSubRepoStub) Update(_ context.Context, subscription *UserSubscription) error {
	if subscription == nil {
		return ErrSubscriptionNilInput
	}
	if s.byID[subscription.ID] == nil {
		return ErrSubscriptionNotFound
	}
	copy := *subscription
	s.byID[copy.ID] = &copy
	return nil
}
