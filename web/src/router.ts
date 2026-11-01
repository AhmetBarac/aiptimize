import { createRouter, createWebHistory } from 'vue-router'
import { bootstrap, session } from './session'

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('./views/DashboardView.vue'),
      meta: { titleKey: 'nav.home' },
    },
    {
      path: '/plan',
      name: 'plan',
      component: () => import('./views/PlanView.vue'),
      meta: { titleKey: 'nav.plan' },
    },
    {
      path: '/prices',
      name: 'prices',
      component: () => import('./views/PricesView.vue'),
      meta: { titleKey: 'nav.prices' },
    },
    {
      path: '/solar',
      name: 'solar',
      component: () => import('./views/SolarView.vue'),
      meta: { titleKey: 'solar.title' },
    },
    {
      path: '/history',
      name: 'history',
      component: () => import('./views/HistoryView.vue'),
      meta: { titleKey: 'nav.history' },
    },
    {
      path: '/system',
      name: 'system',
      component: () => import('./views/SystemView.vue'),
      meta: { titleKey: 'system.title' },
    },
    {
      path: '/battery',
      name: 'battery',
      component: () => import('./views/BatteryView.vue'),
      meta: { titleKey: 'battery.title' },
    },
    {
      path: '/car',
      name: 'car',
      component: () => import('./views/CarView.vue'),
      meta: { titleKey: 'nav.car' },
    },
    {
      path: '/settings/:section?',
      name: 'settings',
      component: () => import('./views/SettingsView.vue'),
      meta: { titleKey: 'nav.settings' },
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('./views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/setup',
      name: 'setup',
      component: () => import('./views/SetupView.vue'),
      meta: { public: true },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('./views/NotFoundView.vue'),
      meta: { public: true },
    },
  ],
  scrollBehavior: () => ({ top: 0 }),
})

router.beforeEach(async (to) => {
  if (!session.bootstrapped) {
    await bootstrap()
  }
  if (session.meta?.setup_required) {
    return to.name === 'setup' ? true : { name: 'setup' }
  }
  if (to.name === 'setup') {
    return { name: session.user ? 'home' : 'login' }
  }
  if (to.meta.public) {
    if (to.name === 'login' && session.user) return { name: 'home' }
    return true
  }
  if (!session.user) {
    return { name: 'login', query: { next: to.fullPath } }
  }
  return true
})
