import { test, expect, type Page } from '@playwright/test'

const admin = {
  username: 'admin',
  password: 'e2e-password-1',
}

const siteName = 'E2E Home'

async function expectNoHorizontalScroll(page: Page): Promise<void> {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  )
  expect(overflow, 'page must not scroll horizontally').toBe(false)
}

async function blurFocus(page: Page): Promise<void> {
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
}

async function signIn(page: Page): Promise<void> {
  await page.goto('/login')
  await page.getByLabel('Username').fill(admin.username)
  await page.getByLabel('Password', { exact: true }).fill(admin.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/$/)
}

async function openMore(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'More' }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
}

async function goFromMore(page: Page, name: RegExp): Promise<void> {
  await openMore(page)
  await page.getByRole('dialog').getByRole('link', { name }).click()
  await expect(page.getByRole('dialog')).toBeHidden()
}

test('first-run setup, home, navigation, settings and login', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByRole('heading', { name: 'Welcome to Aiptimize' })).toBeVisible()

  await page.getByLabel('Username').fill(admin.username)
  await page.getByLabel('Password', { exact: true }).fill(admin.password)
  await page.getByLabel('Site name').fill(siteName)
  await page.getByRole('button', { name: 'Create account and continue' }).click()

  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { level: 1, name: siteName })).toBeVisible()
  await expect(page.locator('.flow-card')).toBeVisible()
  await expect(page.locator('.hero-ring')).toBeVisible()
  await expect(page.locator('.home-forecast').getByRole('heading', { name: 'Next 24 hours' })).toBeVisible()
  await expect(page.locator('.forecast-chart canvas')).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/dashboard-mobile.png', fullPage: false })

  // Tapping the house opens the live source-mix sheet.
  await page.locator('.flow-house').click()
  await expect(page.getByRole('dialog')).toContainText('Home is running on')
  await expect(page.getByRole('dialog')).toContainText('Solar')
  await page.getByRole('dialog').getByRole('button', { name: 'Close' }).click()
  await expect(page.getByRole('dialog')).toBeHidden()

  // The Solar node opens the full production/forecast page.
  await page.locator('.flow-solar').click()
  await expect(page).toHaveURL(/\/solar$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Solar production' })).toBeVisible()
  await expect(page.locator('.solar-chart canvas')).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/solar-mobile.png', fullPage: false })
  await page.getByRole('link', { name: 'Dashboard' }).click()
  await expect(page.getByRole('heading', { level: 1, name: siteName })).toBeVisible()

  // Bottom navigation at 390 px.
  const nav = page.getByRole('navigation', { name: 'Aiptimize' })
  await nav.getByRole('link', { name: 'Plan' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Battery plan' })).toBeVisible()
  await page.getByRole('button', { name: 'Recompute' }).first().click()
  await expect(page.locator('.plan-chart canvas')).toBeVisible({ timeout: 20_000 })
  await expect(page.locator('.plan-timeline')).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/plan-mobile.png', fullPage: false })

  await page.getByRole('button', { name: 'Edit plan' }).click()
  await expect(page.getByRole('button', { name: 'Save draft' })).toBeVisible()
  await page.screenshot({ path: 'e2e/screenshots/plan-editor-mobile.png', fullPage: false })
  await page.getByRole('button', { name: 'Cancel' }).first().click()
  await expect(page.getByRole('button', { name: 'Edit plan' })).toBeVisible()

  await nav.getByRole('link', { name: 'Prices' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Prices' })).toBeVisible()
  await expect(page.locator('.price-chart canvas')).toBeVisible()
  await expect(page.locator('.price-table tbody tr').first()).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/prices-mobile.png', fullPage: false })

  // A manual refresh must report updated days.
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.getByRole('status')).toContainText(/Updated \d+ day\(s\)\./)

  await goFromMore(page, /^History/)
  await expect(page.getByRole('heading', { level: 1, name: 'History' })).toBeVisible()
  await expect(page.locator('.history-chart canvas')).toBeVisible()
  await page.getByRole('button', { name: '30 days' }).click()
  await expect(page.locator('.history-chart canvas')).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/history-mobile.png', fullPage: false })

  await nav.getByRole('link', { name: 'Car' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Car' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Charging mode' })).toBeVisible()
  await expect(page.locator('.mode-card')).toHaveCount(3)
  await expect(page.getByRole('button', { name: 'Save plan' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/car-mobile.png', fullPage: false })

  await goFromMore(page, /^Settings/)
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/settings-mobile.png', fullPage: false })

  // Edit and persist the site name.
  await page.getByLabel('Site name').fill('E2E Home 2')
  await page.getByRole('button', { name: 'Save site settings' }).click()
  await expect(page.getByRole('status')).toContainText('Site settings saved.')

  // Safety section renders both gates.
  await page.getByRole('link', { name: 'Safety', exact: true }).click()
  await expect(page.getByLabel('Arm inverter writes in the app')).toBeVisible()
  await expect(page.getByLabel('Arm EV control in the app')).toBeVisible()

  // The inverter connectivity test reads the simulator without writing.
  await page.getByRole('link', { name: 'Inverter', exact: true }).click()
  await page.getByRole('button', { name: 'Test connection' }).click()
  await expect(page.getByText(/Connected —/)).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/settings-inverter.png', fullPage: false })

  // Solar forecast configuration is reachable from the same section nav.
  await page.getByRole('link', { name: 'Solar', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Add string' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/settings-solar.png', fullPage: false })

  const settingsNav = page.getByRole('navigation', { name: 'Settings sections' })
  await settingsNav.getByRole('link', { name: 'Plan', exact: true }).click()
  await expect(page.getByLabel('Max slots per day')).toHaveValue('4')
  await page.screenshot({ path: 'e2e/screenshots/settings-plan.png', fullPage: false })

  // Desktop layout uses the sidebar and exposes the extra destinations.
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1, name: 'E2E Home 2' })).toBeVisible()
  await expect(nav.getByRole('link', { name: 'History' })).toBeVisible()
  await expect(nav.getByRole('link', { name: 'Settings' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/dashboard-desktop.png', fullPage: false })

  // Sign out from the mobile More sheet and back in.
  await page.setViewportSize({ width: 390, height: 844 })
  await openMore(page)
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await page.getByLabel('Username').fill(admin.username)
  await page.getByLabel('Password', { exact: true }).fill(admin.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { level: 1, name: 'E2E Home 2' })).toBeVisible()
})

test('swedish locale is available', async ({ page }) => {
  await page.goto('/login')
  const localeSelect = page.locator('.auth-locale select')
  await localeSelect.selectOption('sv')
  await expect(page.getByRole('heading', { name: 'Logga in' })).toBeVisible()
  await localeSelect.selectOption('en')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
})

test('theme toggle switches between dark and light', async ({ page }) => {
  await signIn(page)

  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'dark')
  await openMore(page)
  await page.getByRole('button', { name: 'Switch theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'light')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toBeHidden()
  await blurFocus(page)
  await page.screenshot({ path: 'e2e/screenshots/dashboard-light.png', fullPage: false })
  await openMore(page)
  await page.getByRole('button', { name: 'Switch theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'dark')
})

test('swedish home keeps the new layout intact', async ({ page }) => {
  await signIn(page)
  await openMore(page)
  await page.locator('#more-locale').selectOption('sv')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: 'Mer' })).toBeVisible()
  await expect(page.getByText('Energiflöde')).toBeVisible()
  await expectNoHorizontalScroll(page)
  await blurFocus(page)
  await page.screenshot({ path: 'e2e/screenshots/dashboard-swedish.png', fullPage: false })
})

test('home has no horizontal scroll at 360 px', async ({ page }) => {
  await signIn(page)
  await page.setViewportSize({ width: 360, height: 780 })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1 })).toBeVisible()
  await expectNoHorizontalScroll(page)
})
