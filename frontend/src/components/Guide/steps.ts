import { DriveStep } from 'driver.js'

const interactive = ['close'] as const
const navigable = ['next', 'previous'] as const

/** 管理员引导：平台定义路由，账号加入平台池，密钥授权平台与计费资产。 */
export const getAdminSteps = (t: (key: string) => string, isSimpleMode = false): DriveStep[] => {
  const steps: DriveStep[] = [
    {
      popover: {
        title: t('onboarding.admin.welcome.title'),
        description: t('admin.platforms.modelRulesHint'),
        align: 'center',
        nextBtnText: t('common.next'),
        prevBtnText: t('common.back')
      }
    },
    {
      element: '[data-tour="sidebar-platforms"]',
      popover: {
        title: t('nav.platforms'),
        description: t('admin.platforms.endpointCapabilitiesHint'),
        side: 'right',
        align: 'center',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="platforms-create-btn"]',
      popover: {
        title: t('admin.platforms.create'),
        description: t('admin.platforms.modelRulesHint'),
        side: 'bottom',
        align: 'end',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="platform-form-identity"]',
      popover: {
        title: t('admin.platforms.name'),
        description: t('admin.platforms.requiredFields'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="platform-form-models"]',
      popover: {
        title: t('admin.platforms.modelRules'),
        description: t('admin.platforms.modelRulesHint'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="platform-form-submit"]',
      popover: {
        title: t('common.save'),
        description: t('admin.platforms.endpointCapabilitiesHint'),
        side: 'left',
        align: 'center',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="sidebar-accounts"]',
      popover: {
        title: t('nav.accounts'),
        description: t('admin.accounts.platformModelPolicyNotice'),
        side: 'right',
        align: 'center',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="accounts-create-btn"]',
      popover: {
        title: t('admin.accounts.addAccount'),
        description: t('admin.accounts.platformPoolRequired'),
        side: 'bottom',
        align: 'end',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="account-form-name"]',
      popover: {
        title: t('admin.accounts.accountName'),
        description: t('admin.accounts.enterAccountName'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="account-form-platform-pool"]',
      popover: {
        title: t('admin.accounts.platformPool'),
        description: t('admin.accounts.platformModelPolicyNotice'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="account-form-type"]',
      popover: {
        title: t('admin.accounts.accountType'),
        description: t('admin.accounts.accountType'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="account-form-priority"]',
      popover: {
        title: t('admin.accounts.priority'),
        description: t('admin.accounts.priorityHint'),
        side: 'top',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="account-form-submit"]',
      popover: {
        title: t('common.save'),
        description: t('admin.accounts.platformModelPolicyNotice'),
        side: 'left',
        align: 'center',
        showButtons: [...interactive]
      }
    },
    ...getKeySteps(t, true)
  ]

  if (!isSimpleMode) return steps
  return steps.filter(step => {
    const element = typeof step.element === 'string' ? step.element : ''
    return !element.includes('sidebar-platforms') && !element.includes('platform-form-') && !element.includes('platforms-create-btn')
  })
}

function getKeySteps(t: (key: string) => string, admin: boolean): DriveStep[] {
  return [
    {
      element: '[data-tour="sidebar-my-keys"]',
      popover: {
        title: t('nav.apiKeys'),
        description: t('keys.platformsHint'),
        side: 'right',
        align: 'center',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="keys-create-btn"]',
      popover: {
        title: t('keys.createKey'),
        description: t('keys.createFirstKey'),
        side: 'bottom',
        align: 'end',
        showButtons: [...interactive]
      }
    },
    {
      element: '[data-tour="key-form-name"]',
      popover: {
        title: t('keys.nameLabel'),
        description: t('keys.namePlaceholder'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="key-form-platforms"]',
      popover: {
        title: t('keys.platformsLabel'),
        description: t('keys.platformsHint'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="key-form-subscription-plans"]',
      popover: {
        title: t('keys.billingLabel'),
        description: t('keys.billingHint'),
        side: 'right',
        align: 'start',
        showButtons: [...navigable]
      }
    },
    {
      element: '[data-tour="key-form-submit"]',
      popover: {
        title: t('keys.createKey'),
        description: admin ? t('keys.platformsHint') : t('keys.billingHint'),
        side: 'left',
        align: 'center',
        showButtons: [...interactive]
      }
    }
  ]
}

/** 普通用户引导：密钥必须显式授权平台，并选择套餐或余额。 */
export const getUserSteps = (t: (key: string) => string): DriveStep[] => [
  {
    popover: {
      title: t('onboarding.user.welcome.title'),
      description: t('keys.platformsHint'),
      align: 'center',
      nextBtnText: t('common.next'),
      prevBtnText: t('common.back')
    }
  },
  ...getKeySteps(t, false)
]
