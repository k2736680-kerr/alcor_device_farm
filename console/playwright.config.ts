import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  retries: 0,
  workers: 1,
  reporter: [['list']],
  outputDir: '../tmp/playwright-results',
  use: {
    baseURL: 'http://127.0.0.1:18080',
    trace: 'on-first-retry',
  },
  // Use the system-installed Microsoft Edge instead of downloading Playwright's
  // bundled Chromium (~130 MB). channel overrides the executable to msedge.exe.
  projects: [{ name: 'edge', use: { ...devices['Desktop Edge'], channel: 'msedge' } }],
  webServer: {
    command:
      'E:\\AutoTestTools\\Projects\\alcor_device_farm\\tmp\\df-server.exe --config E:\\AutoTestTools\\Projects\\alcor_device_farm\\tmp\\server-config.yaml',
    url: 'http://127.0.0.1:18080/console/',
    reuseExistingServer: true,
    timeout: 30_000,
  },
})
