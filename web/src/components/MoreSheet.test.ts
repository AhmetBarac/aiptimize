import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, describe, expect, it } from 'vitest'
import { i18n } from '../i18n'
import { session } from '../session'
import { ui } from '../ui'
import MoreSheet from './MoreSheet.vue'

const routes = ['/history', '/prices', '/system', '/settings'].map((path) => ({
  path,
  component: { template: '<div />' },
}))

// The sheet teleports to the body, so the rendered text lives there rather than
// inside the component wrapper.
function bodyText(): string {
  return document.body.textContent ?? ''
}

async function mountMore() {
  const router = createRouter({ history: createMemoryHistory(), routes })
  await router.push('/')
  await router.isReady()
  ui.moreOpen = true
  const wrapper = mount(MoreSheet, { global: { plugins: [i18n, router] }, attachTo: document.body })
  await wrapper.vm.$nextTick()
  return wrapper
}

afterEach(() => {
  ui.moreOpen = false
  session.user = null
  document.body.innerHTML = ''
})

describe('MoreSheet', () => {
  it('adds Settings once the session reports an administrator', async () => {
    // The sheet can mount before /auth/me resolves, which is what happens in the
    // companion WebView. Settings must appear when the role arrives.
    session.user = null
    const wrapper = await mountMore()
    expect(bodyText()).not.toContain('Settings')

    session.user = { id: 1, username: 'ahmet', role: 'admin' } as never
    await wrapper.vm.$nextTick()

    expect(bodyText()).toContain('Settings')
    wrapper.unmount()
  })

  it('keeps Settings hidden for a normal user', async () => {
    session.user = { id: 2, username: 'anna', role: 'user' } as never
    const wrapper = await mountMore()
    expect(bodyText()).not.toContain('Settings')
    wrapper.unmount()
  })

  it('always offers History, Prices and System status', async () => {
    session.user = { id: 2, username: 'anna', role: 'user' } as never
    const wrapper = await mountMore()
    const text = bodyText()
    expect(text).toContain('History')
    expect(text).toContain('Prices')
    expect(text).toContain('System status')
    wrapper.unmount()
  })
})
