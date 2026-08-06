import { defineConfig, devices } from '@playwright/test'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const baseURL = process.env.DEVICE_FARM_E2E_BASE_URL ?? 'http://127.0.0.1:18080'
const useExternalServer = process.env.DEVICE_FARM_E2E_EXTERNAL_SERVER === '1'
const consoleRoot = path.dirname(fileURLToPath(import.meta.url))
const projectRoot = path.resolve(consoleRoot, '..')
const defaultServerBinary = path.join(projectRoot, 'tmp', process.platform === 'win32' ? 'df-server.exe' : 'df-server')
const defaultServerConfig = path.join(projectRoot, 'tmp', 'server-config.yaml')
const serverBinary = process.env.DEVICE_FARM_E2E_SERVER_BINARY ?? defaultServerBinary
const serverConfig = process.env.DEVICE_FARM_E2E_SERVER_CONFIG ?? defaultServerConfig
const quote = (value: string) => `"${value.replace(/"/g, '\\"')}"`

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  retries: 0,
  workers: 1,
  reporter: [['list']],
  outputDir: '../tmp/playwright-results',
  use: {
    baseURL,
    ignoreHTTPSErrors: process.env.DEVICE_FARM_E2E_IGNORE_HTTPS_ERRORS === '1',
    trace: 'on-first-retry',
  },
  // Use the system-installed Microsoft Edge instead of downloading Playwright's
  // bundled Chromium (~130 MB). channel overrides the executable to msedge.exe.
  projects: [{ name: 'edge', use: { ...devices['Desktop Edge'], channel: 'msedge' } }],
  webServer: useExternalServer
    ? undefined
    : {
        command:
          `${quote(serverBinary)} --config ${quote(serverConfig)}`,
        url: `${baseURL}/console/`,
        reuseExistingServer: process.env.DEVICE_FARM_E2E_REUSE_SERVER !== '0',
        timeout: 30_000,
      },
})
