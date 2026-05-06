/**
 * Cloudflare Worker: Subscription Proxy
 *
 * Proxies requests to the real panel server so subscription links can use
 * a workers.dev domain (bypasses firewalls that block the server's IP/domain).
 *
 * Setup:
 *   1. Deploy this script to Cloudflare Workers
 *   2. Set the PANEL_URL environment variable (or edit TARGET_ORIGIN below)
 *      to your panel's full base URL, e.g. https://panel.example.com
 *   3. In the panel Settings, set "CF Worker URL" to your worker's URL
 *      e.g. https://my-sub-proxy.myaccount.workers.dev
 *
 * The worker forwards all requests as-is (path, query string, headers) to
 * the real panel and streams the response back to the client unchanged.
 */

// Set TARGET_ORIGIN to your panel's base URL, or use the PANEL_URL env var.
// Example: "https://123.45.67.89:8080" or "https://panel.example.com"
const TARGET_ORIGIN = typeof PANEL_URL !== 'undefined' ? PANEL_URL : 'https://YOUR_PANEL_URL_HERE'

export default {
  async fetch(request, env) {
    const origin = (env && env.PANEL_URL) ? env.PANEL_URL.replace(/\/$/, '') : TARGET_ORIGIN.replace(/\/$/, '')

    const url = new URL(request.url)
    // CF Workers overrides User-Agent on outgoing fetch requests.
    // If the client is Happ, ensure ?client=happ reaches the panel so it
    // returns Happ-specific headers and body params even when Workers rewrites
    // or normalizes User-Agent. Happ-compatible clients commonly send HWID and
    // device headers while importing subscriptions.
    const incomingUA = request.headers.get('user-agent') || ''
    const hasHappDeviceHeaders = ['x-hwid', 'x-device-os', 'x-ver-os', 'x-device-model']
      .some(header => (request.headers.get(header) || '').trim() !== '')
    if (!url.searchParams.has('client') && (incomingUA.toLowerCase().includes('happ') || hasHappDeviceHeaders)) {
      url.searchParams.set('client', 'happ')
    }
    const targetURL = origin + url.pathname + url.search

    // Explicitly copy request headers so client headers (User-Agent, X-Hwid, etc.)
    // reach the panel intact for Happ/Shadowrocket detection.
    const reqHeaders = new Headers()
    for (const [key, value] of request.headers.entries()) {
      if (key.toLowerCase() === 'host') continue
      reqHeaders.set(key, value)
    }

    const proxyRequest = new Request(targetURL, {
      method: request.method,
      headers: reqHeaders,
      body: request.method !== 'GET' && request.method !== 'HEAD' ? request.body : undefined,
      redirect: 'follow',
    })

    try {
      const response = await fetch(proxyRequest)

      // Explicitly copy response headers so all custom subscription headers
      // (profile-title, subscription-userinfo, Happ params, etc.) pass through.
      const resHeaders = new Headers()
      for (const [key, value] of response.headers.entries()) {
        resHeaders.set(key, value)
      }

      return new Response(response.body, {
        status: response.status,
        statusText: response.statusText,
        headers: resHeaders,
      })
    } catch (err) {
      return new Response('Proxy error: ' + err.message, { status: 502 })
    }
  }
}
