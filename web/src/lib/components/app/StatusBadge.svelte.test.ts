import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import StatusBadge from './StatusBadge.svelte';

describe('StatusBadge', () => {
	it('renders active subscription status with correct tone', async () => {
		const screen = render(StatusBadge, {
			kind: 'subscription',
			status: 'active'
		});
		const badge = screen.getByText('Active');
		expect(badge).toBeDefined();
		expect(badge.element().getAttribute('data-status')).toBe('active');
	});

	it('renders past_due subscription status', async () => {
		const screen = render(StatusBadge, {
			kind: 'subscription',
			status: 'past_due'
		});
		expect(screen.getByText('Past Due')).toBeDefined();
	});

	it('renders invoice paid status', async () => {
		const screen = render(StatusBadge, {
			kind: 'invoice',
			status: 'paid'
		});
		expect(screen.getByText('Paid')).toBeDefined();
	});
});
