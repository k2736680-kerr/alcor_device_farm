import { createServer } from 'node:http'

const json = (response, body) => {
  response.writeHead(200, { 'content-type': 'application/json; charset=utf-8' })
  response.end(JSON.stringify(body))
}

createServer((request, response) => {
  const path = new URL(request.url ?? '/', 'http://127.0.0.1').pathname
  if (request.method === 'GET' && path === '/api/v1/devices') {
    json(response, { devices: [{ serial: 'emulator-5554', present: true, ready: true, using: false }] })
    return
  }
  if (request.method === 'POST' && path === '/api/v1/user/devices') {
    json(response, { success: true })
    return
  }
  if (request.method === 'POST' && path.endsWith('/remoteConnect')) {
    json(response, { success: true, remoteConnectUrl: '127.0.0.1:7401' })
    return
  }
  if (request.method === 'DELETE' && path.startsWith('/api/v1/user/devices/')) {
    json(response, { success: true })
    return
  }
  response.writeHead(404, { 'content-type': 'application/json; charset=utf-8' })
  response.end(JSON.stringify({ success: false }))
}).listen(18083, '127.0.0.1')
