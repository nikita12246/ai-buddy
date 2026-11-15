import { defineConfig, devices } from '@playwright/test';

const recordDemo = process.env.RECORD_DEMO === '1';

export default defineConfig({
  testDir: './e2e',
  testMatch: '**/*.e2e.ts',
  timeout: recordDemo ? 90000 : 30000,
  use: {
    baseURL: `http://127.0.0.1:${process.env.BACKEND_DEMO_WEB_PORT || '8080'}`,
    ...devices['Desktop Chrome'],
    video: recordDemo ? { mode: 'on', size: { width: 1280, height: 720 } } : 'off',
  },
});
