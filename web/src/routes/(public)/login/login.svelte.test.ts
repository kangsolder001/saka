import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from 'vitest-browser-svelte';
import LoginPage from './+page.svelte';
import { client } from '$lib/api/client';

vi.mock('$app/state', () => ({
	page: {
		url: new URL('http://localhost:3000/login')
	}
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

describe('LoginPage', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('renders login form properly', async () => {
		const screen = render(LoginPage);

		expect(screen.getByRole('button', { name: /Sign in|Masuk/i })).toBeDefined();
		expect(screen.getByLabelText(/Email/i)).toBeDefined();
		expect(screen.getByLabelText(/Password/i)).toBeDefined();
	});

	it('displays error message on 401 unauthorized', async () => {
		const screen = render(LoginPage);

		vi.spyOn(client, 'POST').mockResolvedValue({
			error: {
				error: {
					code: 'UNAUTHENTICATED',
					message: 'Invalid credentials'
				}
			},
			response: new Response(
				JSON.stringify({
					error: {
						code: 'UNAUTHENTICATED',
						message: 'Invalid credentials'
					}
				}),
				{ status: 401 }
			)
		} as never);

		const emailInput = screen.getByLabelText(/Email/i);
		const passwordInput = screen.getByLabelText(/Password/i);
		const submitBtn = screen.getByRole('button', { name: /Sign in|Masuk/i });

		await emailInput.fill('user@example.com');
		await passwordInput.fill('wrongpassword');
		await submitBtn.click();

		const alert = screen.getByRole('alert');
		expect(alert).toBeDefined();
		expect(alert.element().textContent).toMatch(
			/Email atau password salah|Invalid email or password/i
		);
	});
});
