import { test, expect } from '@playwright/test';

test.describe('Preferences: Theme and Locale (§11.2 Skenario 8)', () => {
	test('switches language to English and theme to dark mode, persisting after reload', async ({
		page
	}) => {
		// Start on the login page where LanguageSwitcher and ThemeSwitcher are present in header
		await page.goto('/login');
		await page.waitForLoadState('networkidle');

		// 1. Language switcher: switch from id to en
		// Trigger button has aria-label="Bahasa" or contains "ID"
		const langTrigger = page.getByRole('button', { name: /bahasa|language/i });
		await expect(langTrigger).toBeVisible();
		await langTrigger.click();

		// Dropdown menu item for English
		const englishOption = page.getByRole('menuitem', { name: 'English' });
		await expect(englishOption).toBeVisible();
		await englishOption.click();

		// Verify locale updated to "en"
		await expect(page.getByRole('button', { name: /language/i })).toBeVisible();
		const savedLocale = await page.evaluate(() => localStorage.getItem('PARAGLIDE_LOCALE'));
		expect(savedLocale).toBe('en');

		// Check an English translation appears on the page (e.g. login submit button or title)
		await expect(page.locator('button[type="submit"]')).toHaveText(/sign in/i);

		// 2. Theme switcher: switch to dark mode
		const themeTrigger = page.getByRole('button', { name: /tema|theme/i });
		await expect(themeTrigger).toBeVisible();
		await themeTrigger.click();

		const darkOption = page.getByRole('menuitem', { name: /dark|gelap/i });
		await expect(darkOption).toBeVisible();
		await darkOption.click();

		// Verify dark mode class added to <html>
		await expect(page.locator('html')).toHaveClass(/dark/);

		// Verify dark mode class added to <html> and saved in localStorage
		await expect(page.locator('html')).toHaveClass(/dark/);
		const savedMode = await page.evaluate(() => localStorage.getItem('mode-watcher-mode'));
		expect(savedMode).toBe('dark');

		// 3. Reload the page
		await page.reload();
		await page.waitForLoadState('networkidle');

		// 4. Verify language remains English and dark theme persists
		await expect(page.locator('html')).toHaveClass(/dark/);
		await expect(page.locator('button[type="submit"]')).toHaveText(/sign in/i);

		const reloadedLocale = await page.evaluate(() => localStorage.getItem('PARAGLIDE_LOCALE'));
		expect(reloadedLocale).toBe('en');

		const reloadedMode = await page.evaluate(() => localStorage.getItem('mode-watcher-mode'));
		expect(reloadedMode).toBe('dark');
	});
});
