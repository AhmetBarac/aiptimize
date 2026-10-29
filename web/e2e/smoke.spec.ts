import { test, expect } from '@playwright/test'

const admin = {
  username: 'admin',
  password: 'e2e-password-1',
}

const siteName = 'E2E Home'

async function expectNoHorizontalScroll(page: import('@playwright/test').Page): Promise<void> {
  const overflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth + 1,
  )
  expect(overflow, 'page must not scroll horizontally at 390 px').toBe(false)
}

test('first-run setup, dashboard, nav, settings and login', async ({ page }) => {
  await page.goto('/')
  await expect(page).toHaveURL(/\/setup$/)
  await expect(page.getByRole('heading', { name: 'Welcome to Aiptimize' })).toBeVisible()

  await page.getByLabel('Username').fill(admin.username)
  await page.getByLabel('Password', { exact: true }).fill(admin.password)
  await page.getByLabel('Site name').fill(siteName)
  await page.getByRole('button', { name: 'Create account and continue' }).click()

  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible()
  await expect(page.locator('.soc-ring span')).toContainText('%')
  await expect(page.locator('.energy-tile').first()).not.toContainText('—')
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/dashboard-mobile.png', fullPage: true })

  // Bottom navigation at 390 px.
  const nav = page.getByRole('navigation', { name: 'Aiptimize' })
  await nav.getByRole('link', { name: 'Plan' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Battery plan' })).toBeVisible()
  await expectNoHorizontalScroll(page)

  await nav.getByRole('link', { name: 'Prices' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Prices' })).toBeVisible()
  await expect(page.locator('.price-chart canvas')).toBeVisible()
  await expect(page.locator('.price-table tbody tr').first()).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/prices-mobile.png', fullPage: true })

  // A manual refresh must report updated days.
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(page.getByRole('status')).toContainText(/Updated \d+ day\(s\)\./)

  await nav.getByRole('link', { name: 'Car' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Car' })).toBeVisible()

  await nav.getByRole('link', { name: 'Settings' }).click()
  await expect(page.getByRole('heading', { level: 1, name: 'Settings' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/settings-mobile.png', fullPage: true })

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
  await page.screenshot({ path: 'e2e/screenshots/settings-inverter.png', fullPage: true })

  // Desktop layout uses the sidebar.
  await page.setViewportSize({ width: 1280, height: 800 })
  await page.goto('/')
  await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible()
  await expectNoHorizontalScroll(page)
  await page.screenshot({ path: 'e2e/screenshots/dashboard-desktop.png', fullPage: true })

  // Sign out and back in.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'Sign out' }).click()
  await expect(page).toHaveURL(/\/login$/)
  await page.getByLabel('Username').fill(admin.username)
  await page.getByLabel('Password', { exact: true }).fill(admin.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Dashboard' })).toBeVisible()
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
  await page.goto('/login')
  await page.getByLabel('Username').fill(admin.username)
  await page.getByLabel('Password', { exact: true }).fill(admin.password)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/$/)

  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'dark')
  await page.getByRole('button', { name: 'Switch theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'light')
  await page.screenshot({ path: 'e2e/screenshots/dashboard-light.png', fullPage: false })
  await page.getByRole('button', { name: 'Switch theme' }).click()
  await expect(page.locator('html')).toHaveAttribute('data-bs-theme', 'dark')
})
