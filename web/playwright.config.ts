import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
	testDir: 'tests/e2e',
	testMatch: /.*\.spec\.ts/,
	fullyParallel: false,
	workers: 1,
	use: {
		baseURL: 'http://localhost:3000',
		trace: 'on-first-retry'
	},
	projects: [
		{
			name: 'chromium',
			use: { ...devices['Desktop Chrome'] }
		}
	],
	webServer: {
		command: 'pnpm dev',
		cwd: import.meta.dirname,
		port: 3000,
		reuseExistingServer: !process.env.CI
	}
});
