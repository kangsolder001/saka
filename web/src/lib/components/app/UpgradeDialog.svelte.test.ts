import { describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import UpgradeDialog from './UpgradeDialog.svelte';

describe('UpgradeDialog', () => {
	it('renders title, description and upgrade button when open', async () => {
		const screen = render(UpgradeDialog, {
			open: true,
			orgId: 'org-test'
		});

		await vi.waitFor(() => {
			const dialog = screen.getByTestId('upgrade-dialog');
			expect(dialog).toBeDefined();
		});

		const btn = screen.getByTestId('upgrade-dialog-btn');
		expect(btn.element().getAttribute('href')).toBe('/orgs/org-test/billing/plans');
	});
});
