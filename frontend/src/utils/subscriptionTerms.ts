import type { UserSubscription } from '@/types'

export type SubscriptionLimitWindow = 'daily' | 'weekly' | 'monthly'

export function getSubscriptionPlanName(subscription: UserSubscription): string {
  return subscription.plan_name_snapshot || `Plan #${subscription.subscription_plan_id}`
}

export function getSubscriptionLimit(
  subscription: UserSubscription,
  window: SubscriptionLimitWindow,
): number | null {
  return getSnapshotLimit(subscription, window) ?? null
}

export function getSubscriptionRateMultiplier(subscription: UserSubscription): number {
  return subscription.rate_multiplier_snapshot ?? 1
}

function getSnapshotLimit(
  subscription: UserSubscription,
  window: SubscriptionLimitWindow,
): number | null | undefined {
  if (window === 'daily') return subscription.daily_limit_usd_snapshot
  if (window === 'weekly') return subscription.weekly_limit_usd_snapshot
  return subscription.monthly_limit_usd_snapshot
}
