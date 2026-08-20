package service

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

const subscriptionDayDuration = 24 * time.Hour

type UserSubscription struct {
	ID     int64
	UserID int64

	SubscriptionPlanID      *int64
	PlanNameSnapshot        string
	DailyLimitUSDSnapshot   *float64
	WeeklyLimitUSDSnapshot  *float64
	MonthlyLimitUSDSnapshot *float64
	RateMultiplierSnapshot  float64

	StartsAt  time.Time
	ExpiresAt time.Time
	Status    string

	DailyWindowStart   *time.Time
	WeeklyWindowStart  *time.Time
	MonthlyWindowStart *time.Time

	DailyUsageUSD   float64
	WeeklyUsageUSD  float64
	MonthlyUsageUSD float64

	AssignedBy *int64
	AssignedAt time.Time
	Notes      string

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time

	User           *User
	AssignedByUser *User
}

func (s *UserSubscription) IsActive() bool {
	return s.Status == SubscriptionStatusActive && time.Now().Before(s.ExpiresAt)
}

func (s *UserSubscription) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *UserSubscription) DaysRemaining() int {
	return s.daysRemainingAt(time.Now())
}

func (s *UserSubscription) daysRemainingAt(now time.Time) int {
	remaining := s.ExpiresAt.Sub(now)
	if remaining <= 0 {
		return 0
	}

	days := int(remaining / subscriptionDayDuration)
	if remaining%subscriptionDayDuration != 0 {
		days++
	}
	return days
}

func (s *UserSubscription) IsWindowActivated() bool {
	return s.DailyWindowStart != nil || s.WeeklyWindowStart != nil || s.MonthlyWindowStart != nil
}

func (s *UserSubscription) HasOneTimeDailyQuota() bool {
	if s == nil || s.StartsAt.IsZero() || s.ExpiresAt.IsZero() {
		return false
	}
	return !s.ExpiresAt.After(s.StartsAt.AddDate(0, 0, 1))
}

func (s *UserSubscription) NeedsDailyReset() bool {
	return s.NeedsDailyResetAt(time.Now())
}

func (s *UserSubscription) NeedsDailyResetAt(now time.Time) bool {
	_, ok := s.automaticDailyWindowStartAt(now)
	return ok
}

func (s *UserSubscription) NeedsWeeklyReset() bool {
	return s.NeedsWeeklyResetAt(time.Now())
}

func (s *UserSubscription) NeedsWeeklyResetAt(now time.Time) bool {
	if s.WeeklyWindowStart == nil {
		return false
	}
	return !now.Before(s.WeeklyWindowStart.Add(7 * 24 * time.Hour))
}

func (s *UserSubscription) NeedsMonthlyReset() bool {
	return s.NeedsMonthlyResetAt(time.Now())
}

func (s *UserSubscription) NeedsMonthlyResetAt(now time.Time) bool {
	if s.MonthlyWindowStart == nil {
		return false
	}
	return !now.Before(s.MonthlyWindowStart.Add(30 * 24 * time.Hour))
}

func (s *UserSubscription) canAutomaticallyResetDailyAt(now time.Time) bool {
	_, ok := s.automaticDailyWindowStartAt(now)
	return ok
}

func (s *UserSubscription) automaticDailyWindowStartAt(now time.Time) (time.Time, bool) {
	if s.DailyWindowStart == nil || s.HasOneTimeDailyQuota() {
		return time.Time{}, false
	}
	today := timezone.StartOfDay(now)
	if !today.After(timezone.StartOfDay(*s.DailyWindowStart)) {
		return time.Time{}, false
	}
	return today, true
}

func (s *UserSubscription) canAutomaticallyResetWeeklyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.WeeklyWindowStart, 7*24*time.Hour, now)
	return ok
}

func (s *UserSubscription) canAutomaticallyResetMonthlyAt(now time.Time) bool {
	_, ok := s.automaticWindowStartAt(s.MonthlyWindowStart, 30*24*time.Hour, now)
	return ok
}

func (s *UserSubscription) automaticWindowStartAt(previous *time.Time, period time.Duration, now time.Time) (time.Time, bool) {
	if previous == nil {
		return time.Time{}, false
	}

	anchor := *previous
	// Older subscriptions initialized their first windows at midnight on their
	// start date. Only that initial value is unambiguous; later midnight anchors
	// may be manual resets and must remain authoritative.
	legacyAnchor := startOfDay(s.StartsAt)
	if legacyAnchor.Before(s.StartsAt) && anchor.Equal(legacyAnchor) {
		anchor = s.StartsAt
	}
	next := anchor.Add(period)
	if now.Before(next) || !next.Before(s.ExpiresAt) {
		return time.Time{}, false
	}

	periods := now.Sub(anchor) / period
	lastPeriodBeforeExpiry := (s.ExpiresAt.Sub(anchor) - 1) / period
	if periods > lastPeriodBeforeExpiry {
		periods = lastPeriodBeforeExpiry
	}
	return anchor.Add(periods * period), true
}

func (s *UserSubscription) DailyResetTime() *time.Time {
	if s.DailyWindowStart == nil {
		return nil
	}
	if s.HasOneTimeDailyQuota() {
		t := s.ExpiresAt
		return &t
	}
	t := timezone.StartOfDay(*s.DailyWindowStart).AddDate(0, 0, 1)
	return &t
}

func (s *UserSubscription) WeeklyResetTime() *time.Time {
	if s.WeeklyWindowStart == nil {
		return nil
	}
	t := s.WeeklyWindowStart.Add(7 * 24 * time.Hour)
	return &t
}

func (s *UserSubscription) MonthlyResetTime() *time.Time {
	if s.MonthlyWindowStart == nil {
		return nil
	}
	t := s.MonthlyWindowStart.Add(30 * 24 * time.Hour)
	return &t
}

func (s *UserSubscription) CheckDailyLimit(additionalCost float64) bool {
	limit := s.dailyLimitUSD()
	return !hasUsageLimit(limit) || s.DailyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckWeeklyLimit(additionalCost float64) bool {
	limit := s.weeklyLimitUSD()
	return !hasUsageLimit(limit) || s.WeeklyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckMonthlyLimit(additionalCost float64) bool {
	limit := s.monthlyLimitUSD()
	return !hasUsageLimit(limit) || s.MonthlyUsageUSD+additionalCost <= *limit
}

func (s *UserSubscription) CheckAllLimits(additionalCost float64) (daily, weekly, monthly bool) {
	daily = s.CheckDailyLimit(additionalCost)
	weekly = s.CheckWeeklyLimit(additionalCost)
	monthly = s.CheckMonthlyLimit(additionalCost)
	return
}

func (s *UserSubscription) dailyLimitUSD() *float64 {
	if s != nil && s.DailyLimitUSDSnapshot != nil {
		return s.DailyLimitUSDSnapshot
	}
	return nil
}

// DailyLimitUSD returns the immutable plan limit snapshot.
func (s *UserSubscription) DailyLimitUSD() *float64 {
	return s.dailyLimitUSD()
}

func (s *UserSubscription) weeklyLimitUSD() *float64 {
	if s != nil && s.WeeklyLimitUSDSnapshot != nil {
		return s.WeeklyLimitUSDSnapshot
	}
	return nil
}

// WeeklyLimitUSD returns the immutable plan limit snapshot.
func (s *UserSubscription) WeeklyLimitUSD() *float64 {
	return s.weeklyLimitUSD()
}

func (s *UserSubscription) monthlyLimitUSD() *float64 {
	if s != nil && s.MonthlyLimitUSDSnapshot != nil {
		return s.MonthlyLimitUSDSnapshot
	}
	return nil
}

// MonthlyLimitUSD returns the immutable plan limit snapshot.
func (s *UserSubscription) MonthlyLimitUSD() *float64 {
	return s.monthlyLimitUSD()
}

func hasUsageLimit(limit *float64) bool {
	return limit != nil && *limit > 0
}
