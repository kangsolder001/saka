import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import EmailVerificationBanner from './EmailVerificationBanner.svelte';

describe('EmailVerificationBanner', () => {
	it('renders unverified email warning message and resend button', async () => {
		const screen = render(EmailVerificationBanner);
		expect(
			screen.getByText(
				'Please verify your email. Some features (inviting members, subscriptions) are unavailable until verified.'
			)
		).toBeDefined();
		expect(screen.getByRole('button', { name: 'Resend' })).toBeDefined();
	});
});
