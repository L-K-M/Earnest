self.addEventListener('push', event => {
  if (!event.data) return
  let payload
  try { payload = event.data.json() } catch { return }
  if (typeof payload.title !== 'string' || typeof payload.body !== 'string') return
  const id = typeof payload.notification_id === 'string' && /^[1-9]\d*$/.test(payload.notification_id) ? payload.notification_id : ''
  event.waitUntil(self.registration.showNotification(payload.title, {body: payload.body, data: {id}}))
})

self.addEventListener('notificationclick', event => {
  event.notification.close()
  const id = event.notification.data?.id
  const path = typeof id === 'string' && /^[1-9]\d*$/.test(id) ? `/?notification=${id}` : '/'
  event.waitUntil(self.clients.openWindow(path))
})
