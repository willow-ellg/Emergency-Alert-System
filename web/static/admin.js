const TOKEN_KEY = 'emergency-hub-admin-token'

const tokenInput = document.getElementById('admin-token')
const status = document.getElementById('admin-status')
const reportsList = document.getElementById('reports-list')
const alertsList = document.getElementById('alerts-list')

function setStatus(message) {
  if (status) status.textContent = message
}

function getToken() {
  if (tokenInput && tokenInput.value.trim()) return tokenInput.value.trim()
  try {
    return window.sessionStorage.getItem(TOKEN_KEY) || ''
  } catch {
    return ''
  }
}

function saveToken() {
  const token = tokenInput ? tokenInput.value.trim() : ''
  try {
    window.sessionStorage.setItem(TOKEN_KEY, token)
  } catch {
    // Ignore storage failures; the in-memory value still works.
  }
  setStatus(token ? 'Operator token saved for this tab.' : 'Operator token cleared.')
}

async function requestJSON(path, options = {}) {
  const token = getToken()
  if (!token) {
    throw new Error('Enter the operator token first.')
  }

  const headers = new Headers(options.headers || {})
  headers.set('Authorization', 'Bearer ' + token)
  if (options.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }

  const response = await fetch(path, { ...options, headers })
  const result = await response.json().catch(() => ({}))
  if (!response.ok) {
    if (response.status === 401) {
      throw new Error('Token rejected. Check that HUB_ADMIN_TOKEN matches.')
    }
    throw new Error(result.error || 'Request failed with status ' + response.status + '.')
  }
  return result
}

function element(tag, className, text) {
  const node = document.createElement(tag)
  if (className) node.className = className
  if (text !== undefined) node.textContent = text
  return node
}

function timeLabel(value) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return date.toLocaleString()
}

function renderReports(reports) {
  if (!reportsList) return
  reportsList.replaceChildren()

  if (!reports.length) {
    reportsList.append(element('p', 'govuk-body', 'No pending reports.'))
    return
  }

  reports.forEach((report) => {
    const card = element('article', 'govuk-summary-card')
    const wrapper = element('div', 'govuk-summary-card__title-wrapper')
    const title = element(
      'h3',
      'govuk-summary-card__title',
      report.area + ' - ' + report.category
    )
    wrapper.append(title)

    const content = element('div', 'govuk-summary-card__content')
    content.append(element('p', 'govuk-body', report.body))
    content.append(element('p', 'govuk-body-s', 'Submitted ' + timeLabel(report.created_at)))

    const group = element('div', 'govuk-button-group')
    const approve = element('button', 'govuk-button', 'Approve and publish')
    approve.type = 'button'
    approve.dataset.action = 'approve'
    approve.dataset.id = report.id

    const reject = element('button', 'govuk-button govuk-button--warning', 'Reject')
    reject.type = 'button'
    reject.dataset.action = 'reject'
    reject.dataset.id = report.id

    group.append(approve, reject)
    content.append(group)
    card.append(wrapper, content)
    reportsList.append(card)
  })
}

function renderAlerts(alerts) {
  if (!alertsList) return
  alertsList.replaceChildren()

  if (!alerts.length) {
    alertsList.append(element('p', 'govuk-body', 'No alerts.'))
    return
  }

  alerts.forEach((alert) => {
    const card = element('article', 'govuk-summary-card')
    const wrapper = element('div', 'govuk-summary-card__title-wrapper')
    const title = element('h3', 'govuk-summary-card__title', alert.title)
    const actions = element('ul', 'govuk-summary-card__actions')
    const action = element('li', 'govuk-summary-card__action')
    action.append(element('span', 'govuk-tag govuk-tag--grey', alert.severity))
    actions.append(action)
    wrapper.append(title, actions)

    const content = element('div', 'govuk-summary-card__content')
    content.append(element('p', 'govuk-body', alert.summary))
    content.append(
      element(
        'p',
        'govuk-body-s',
        (alert.active ? 'Active' : 'Inactive') +
          ' - updated ' +
          timeLabel(alert.updated_at)
      )
    )

    const remove = element('button', 'govuk-button govuk-button--warning', 'Delete alert')
    remove.type = 'button'
    remove.dataset.action = 'delete'
    remove.dataset.id = alert.id
    content.append(remove)

    card.append(wrapper, content)
    alertsList.append(card)
  })
}

async function refreshReports() {
  setStatus('Loading pending reports...')
  try {
    const reports = await requestJSON('/api/admin/reports?status=pending')
    renderReports(reports || [])
    setStatus('Pending reports loaded.')
  } catch (error) {
    setStatus(error.message)
  }
}

async function refreshAlerts() {
  setStatus('Loading alerts...')
  try {
    const alerts = await requestJSON('/api/admin/alerts')
    renderAlerts(alerts || [])
    setStatus('Alerts loaded.')
  } catch (error) {
    setStatus(error.message)
  }
}

async function moderateReport(id, action) {
  setStatus('Saving decision...')
  try {
    await requestJSON('/api/admin/reports/' + encodeURIComponent(id) + '/' + action, {
      method: 'POST',
      body: JSON.stringify({ verified: action === 'approve' }),
    })
    setStatus(action === 'approve' ? 'Report approved and published.' : 'Report rejected.')
    await refreshReports()
  } catch (error) {
    setStatus(error.message)
  }
}

async function deleteAlert(id) {
  setStatus('Deleting alert...')
  try {
    await requestJSON('/api/admin/alerts/' + encodeURIComponent(id), { method: 'DELETE' })
    setStatus('Alert deleted.')
    await refreshAlerts()
  } catch (error) {
    setStatus(error.message)
  }
}

async function publishAlert(event) {
  event.preventDefault()
  const form = event.currentTarget

  const guidance = (form.querySelector('#alert-guidance')?.value || '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)

  const areas = (form.querySelector('#alert-areas')?.value || '')
    .split(',')
    .map((area) => area.trim())
    .filter(Boolean)

  const number = (selector) => {
    const value = form.querySelector(selector)?.value
    return value ? Number(value) : 0
  }

  const payload = {
    title: form.querySelector('#alert-title')?.value.trim() || '',
    severity: form.querySelector('#alert-severity')?.value || 'info',
    summary: form.querySelector('#alert-summary')?.value.trim() || '',
    guidance,
    areas,
    lat: number('#alert-lat'),
    lon: number('#alert-lon'),
    radius_km: number('#alert-radius'),
    source: form.querySelector('#alert-source')?.value.trim() || 'Emergency Response Hub',
    official: true,
    active: form.querySelector('#alert-active')?.checked ?? true,
  }

  if (!payload.title || !payload.summary) {
    setStatus('A title and summary are required.')
    return
  }

  setStatus('Publishing alert...')
  try {
    const created = await requestJSON('/api/admin/alerts', {
      method: 'POST',
      body: JSON.stringify(payload),
    })
    setStatus('Alert published: ' + created.title)
    form.reset()
    await refreshAlerts()
  } catch (error) {
    setStatus(error.message)
  }
}

async function exportSnapshot() {
  setStatus('Generating emergency snapshot...')
  try {
    const result = await requestJSON('/api/admin/snapshot', { method: 'POST' })
    setStatus(
      'Snapshot written to ' +
        result.directory +
        ' (' +
        result.alerts +
        ' active alerts, generated ' +
        timeLabel(result.generated_at) +
        ').'
    )
  } catch (error) {
    setStatus(error.message)
  }
}

function setup() {
  try {
    if (tokenInput) tokenInput.value = window.sessionStorage.getItem(TOKEN_KEY) || ''
  } catch {
    // Ignore storage failures.
  }

  document.getElementById('save-token')?.addEventListener('click', saveToken)
  document.getElementById('alert-form')?.addEventListener('submit', publishAlert)
  document.getElementById('refresh-reports')?.addEventListener('click', refreshReports)
  document.getElementById('refresh-alerts')?.addEventListener('click', refreshAlerts)
  document.getElementById('export-snapshot')?.addEventListener('click', exportSnapshot)

  reportsList?.addEventListener('click', (event) => {
    const button = event.target.closest('button[data-action]')
    if (!button) return
    const id = button.dataset.id
    const action = button.dataset.action
    if (action === 'approve' || action === 'reject') moderateReport(id, action)
  })

  alertsList?.addEventListener('click', (event) => {
    const button = event.target.closest('button[data-action="delete"]')
    if (!button) return
    deleteAlert(button.dataset.id)
  })
}

setup()
