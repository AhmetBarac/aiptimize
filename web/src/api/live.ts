import type { Status } from './types'

/**
 * Subscribes to the existing status stream. EventSource reconnects on its own;
 * callers keep a polling fallback while the connection is not open.
 */
export function openStatusStream(
  onStatus: (status: Status) => void,
  onConnection: (connected: boolean) => void,
): () => void {
  if (typeof EventSource === 'undefined') return () => {}

  const source = new EventSource('/api/v1/stream')
  source.onopen = () => onConnection(true)
  source.onerror = () => onConnection(false)
  source.addEventListener('status', (event) => {
    try {
      onStatus(JSON.parse((event as MessageEvent).data) as Status)
    } catch {
      // Ignore malformed frames; the next frame or polling will recover.
    }
  })

  return () => source.close()
}
