import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import Banner from './Banner.svelte';

describe('Banner', () => {
	it('renders default info banner with correct role and variant attribute', async () => {
		const screen = render(Banner, {
			variant: 'info'
		});
		const banner = screen.getByRole('status');
		expect(banner.element().getAttribute('data-variant')).toBe('info');
	});

	it('renders warning banner', async () => {
		const screen = render(Banner, {
			variant: 'warning'
		});
		const banner = screen.getByRole('status');
		expect(banner.element().getAttribute('data-variant')).toBe('warning');
	});
});
