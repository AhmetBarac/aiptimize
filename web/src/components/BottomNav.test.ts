import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it } from 'vitest'
import { i18n } from '../i18n'
import { ui } from '../ui'
import BottomNav from './BottomNav.vue'
import MoreSheet from './MoreSheet.vue'

const routes = ['/', '/plan', '/solar', '/prices', '/car', '/history', '/settings', '/system'].map((path) => ({
  path,
  component: { template: '<div />' },
}))

async function mountNav() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/')
  await router.isReady()
  return mount(BottomNav, { global: { plugins: [i18n, router] } })
}

afterEach(() => {
  ui.moreOpen = false
  document.body.innerHTML = ''
})

describe('BottomNav', () => {
  it('lists Solar instead of Prices in the primary slots', async () => {
    const wrapper = await mountNav()
    const primary = wrapper.findAll('.nav-link').filter((link) => !link.classes('nav-extra'))
    const hrefs = primary.map((link) => link.attributes('href'))

    expect(hrefs).toEqual(['/plan', '/solar', '/', '/car'])
    expect(primary.map((link) => link.text())).toContain('Solar')
    expect(wrapper.html()).not.toContain('href="/prices"')
    wrapper.unmount()
  })

  it('keeps Prices reachable from the More sheet', async () => {
    ui.moreOpen = true
    const router = createRouter({ history: createMemoryHistory(), routes })
    await router.push('/')
    await router.isReady()
    const wrapper = mount(MoreSheet, { global: { plugins: [i18n, router] }, attachTo: document.body })

    const prices = Array.from(document.querySelectorAll('a[href="/prices"]'))
    expect(prices).toHaveLength(1)
    expect(prices[0].textContent).toContain('Prices')
    expect(document.querySelectorAll('a[href="/solar"]')).toHaveLength(0)

    wrapper.unmount()
  })
})
