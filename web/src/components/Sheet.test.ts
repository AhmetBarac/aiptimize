import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { i18n } from '../i18n'
import Sheet from './Sheet.vue'

function mountSheet(open = true) {
  return mount(Sheet, {
    props: { open, title: 'Test sheet' },
    global: { plugins: [i18n] },
    attachTo: document.body,
  })
}

afterEach(() => {
  document.body.innerHTML = ''
  document.body.style.overflow = ''
})

describe('Sheet', () => {
  it('renders a labelled dialog and closes on Escape', async () => {
    const wrapper = mountSheet()
    const dialog = document.querySelector('[role="dialog"]')
    expect(dialog).not.toBeNull()
    expect(dialog?.getAttribute('aria-modal')).toBe('true')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    expect(wrapper.emitted('update:open')?.[0]).toEqual([false])
    expect(document.body.style.overflow).toBe('hidden')
    await wrapper.setProps({ open: false })
    expect(document.body.style.overflow).toBe('')
    wrapper.unmount()
  })

  it('renders slot content and footer', () => {
    const wrapper = mount(Sheet, {
      props: { open: true, title: 'Test sheet' },
      slots: { default: '<p>Body</p>', footer: '<button>Done</button>' },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    expect(document.body.textContent).toContain('Body')
    expect(document.body.textContent).toContain('Done')
    wrapper.unmount()
  })
})
