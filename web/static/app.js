import { initAll } from './govuk/govuk-frontend.min.js'

initAll()

const CONTRAST_KEY = 'emergency-hub-contrast'
const TEXT_KEY = 'emergency-hub-large-text'

const liveRegion = document.getElementById('live-updates')

function announce(message) {
  if (!liveRegion) return
  liveRegion.textContent = message
}

function readPreference(key) {
  try {
    return window.localStorage.getItem(key) === 'true'
  } catch {
    return false
  }
}

function savePreference(key, value) {
  try {
    window.localStorage.setItem(key, String(value))
  } catch {
    // Storage can be unavailable in private modes. Preferences still work
    // for this page view.
  }
}

function applyPreferences() {
  const contrast = readPreference(CONTRAST_KEY)
  const largeText = readPreference(TEXT_KEY)

  document.body.classList.toggle('app-high-contrast', contrast)
  document.documentElement.classList.toggle('app-large-text', largeText)

  const contrastButton = document.getElementById('toggle-contrast')
  if (contrastButton) contrastButton.setAttribute('aria-pressed', String(contrast))

  const textButton = document.getElementById('toggle-text')
  if (textButton) textButton.setAttribute('aria-pressed', String(largeText))
}

function setupPreferenceToggles() {
  const contrastButton = document.getElementById('toggle-contrast')
  if (contrastButton) {
    contrastButton.addEventListener('click', () => {
      const next = !document.body.classList.contains('app-high-contrast')
      document.body.classList.toggle('app-high-contrast', next)
      contrastButton.setAttribute('aria-pressed', String(next))
      savePreference(CONTRAST_KEY, next)
      announce(next ? 'High contrast mode on.' : 'High contrast mode off.')
    })
  }

  const textButton = document.getElementById('toggle-text')
  if (textButton) {
    textButton.addEventListener('click', () => {
      const next = !document.documentElement.classList.contains('app-large-text')
      document.documentElement.classList.toggle('app-large-text', next)
      textButton.setAttribute('aria-pressed', String(next))
      savePreference(TEXT_KEY, next)
      announce(next ? 'Larger text on.' : 'Larger text off.')
    })
  }
}

function setupLocation() {
  const button = document.getElementById('use-location')
  const status = document.getElementById('location-status')
  const area = document.getElementById('area')
  const lat = document.getElementById('lat')
  const lon = document.getElementById('lon')
  const form = document.getElementById('location-form')

  if (!button || !form || !lat || !lon) return

  if (area) {
    area.addEventListener('input', () => {
      lat.value = ''
      lon.value = ''
    })
  }

  button.addEventListener('click', () => {
    if (!('geolocation' in navigator)) {
      if (status) status.textContent = 'Location is not available in this browser. Enter an area instead.'
      return
    }

    if (status) status.textContent = 'Getting your location...'

    navigator.geolocation.getCurrentPosition(
      (position) => {
        lat.value = position.coords.latitude.toFixed(6)
        lon.value = position.coords.longitude.toFixed(6)
        if (area) area.value = ''
        if (status) status.textContent = 'Location found. Showing nearby alerts.'
        form.submit()
      },
      () => {
        if (status) {
          status.textContent =
            'We could not get your location. Check your browser permissions or enter an area instead.'
        }
      },
      { enableHighAccuracy: false, timeout: 10000, maximumAge: 300000 }
    )
  })
}

function setupReportForm() {
  const form = document.getElementById('report-form')
  if (!form) return

  const status = document.getElementById('report-status')
  const submit = form.querySelector('button[type="submit"]')

  form.addEventListener('submit', async (event) => {
    event.preventDefault()

    const body = {
      area: form.querySelector('#report-area')?.value.trim() ?? '',
      category: form.querySelector('#report-category')?.value ?? 'other',
      alert_id: form.querySelector('#report-alert')?.value ?? '',
      body: form.querySelector('#report-body')?.value.trim() ?? '',
    }

    if (!body.area || !body.body) {
      if (status) status.textContent = 'Add an area and a description before sending.'
      return
    }

    if (submit) submit.disabled = true
    if (status) status.textContent = 'Sending your report...'

    try {
      const response = await fetch('/api/reports', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      const result = await response.json().catch(() => ({}))
      if (!response.ok) {
        throw new Error(result.error || 'The report could not be sent.')
      }
      form.reset()
      if (status) {
        status.textContent =
          'Thank you. Your report has been sent for checking and will appear once verified.'
      }
      announce('Report sent for checking.')
    } catch (error) {
      if (status) status.textContent = error.message
    } finally {
      if (submit) submit.disabled = false
    }
  })
}

function setupLiveUpdates() {
  const alertsRegion = document.getElementById('alerts-region')
  const reportsRegion = document.getElementById('reports-region')
  if (!alertsRegion || !reportsRegion || !('EventSource' in window)) return

  const source = new EventSource('/api/stream')
  let announced = false

  source.addEventListener('update', async () => {
    try {
      const query = window.location.search
      const [alertsResponse, reportsResponse] = await Promise.all([
        fetch('/partials/alerts' + query, { headers: { Accept: 'text/html' } }),
        fetch('/partials/reports' + query, { headers: { Accept: 'text/html' } }),
      ])
      if (alertsResponse.ok) alertsRegion.innerHTML = await alertsResponse.text()
      if (reportsResponse.ok) reportsRegion.innerHTML = await reportsResponse.text()
      announce('Alerts and community updates refreshed.')
      announced = true
    } catch {
      if (!announced) announce('Live updates are paused. Refresh the page for the latest information.')
    }
  })

  source.addEventListener('error', () => {
    if (!announced) announce('Live updates are paused. Refresh the page for the latest information.')
  })
}

applyPreferences()
setupPreferenceToggles()
setupLocation()
setupReportForm()
setupLiveUpdates()
