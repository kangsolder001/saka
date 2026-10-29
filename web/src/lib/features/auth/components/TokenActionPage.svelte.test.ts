import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from 'vitest-browser-svelte';
import TokenActionPage from './TokenActionPage.svelte';

describe('TokenActionPage', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('renders loading state initially and then success state upon action resolve', async () => {
		const action = vi.fn().mockResolvedValue(undefined);

		const screen = render(TokenActionPage, {
			title: 'Verifying',
			loadingText: 'Please wait...',
			successTitle: 'Success!',
			successMessage: 'Action was completed',
			primaryButtonLabel: 'Continue',
			primaryButtonHref: '/login',
			action
		});

		expect(action).toHaveBeenCalledTimes(1);

		// Wait for state transition to success
		await vi.waitFor(() => {
			expect(screen.getByText('Success!')).toBeDefined();
			expect(screen.getByText('Action was completed')).toBeDefined();
			expect(screen.getByRole('button', { name: 'Continue' })).toBeDefined();
		});
	});

	it('renders error state upon action failure', async () => {
		const action = vi.fn().mockRejectedValue(new Error('Failed action'));

		const screen = render(TokenActionPage, {
			title: 'Verifying',
			loadingText: 'Please wait...',
			successTitle: 'Success!',
			successMessage: 'Action was completed',
			primaryButtonLabel: 'Continue',
			primaryButtonHref: '/login',
			action
		});

		expect(action).toHaveBeenCalledTimes(1);

		await vi.waitFor(() => {
			const alert = screen.getByRole('alert');
			expect(alert).toBeDefined();
			expect(alert.element().textContent).toContain('Failed action');
		});
	});
});
