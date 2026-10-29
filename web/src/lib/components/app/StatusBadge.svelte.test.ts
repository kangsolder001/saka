import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import * as m from '$lib/paraglide/messages.js';
import StatusBadge from './StatusBadge.svelte';

describe('StatusBadge', () => {
	it('renders active subscription status with correct tone', async () => {
		const screen = render(StatusBadge, {
			kind: 'subscription',
			status: 'active'
		});
		const badge = screen.getByText(m.badge_status_active());
		expect(badge).toBeDefined();
		expect(badge.element().getAttribute('data-status')).toBe('active');
		expect(badge.element().className).toContain('bg-success-muted');
		expect(badge.element().className).toContain('text-success');
	});

	it('renders past_due subscription status', async () => {
		const screen = render(StatusBadge, {
			kind: 'subscription',
			status: 'past_due'
		});
		expect(screen.getByText(m.badge_status_past_due())).toBeDefined();
	});

	it('renders invoice paid status', async () => {
		const screen = render(StatusBadge, {
			kind: 'invoice',
			status: 'paid'
		});
		expect(screen.getByText(m.badge_status_paid())).toBeDefined();
	});
});
