import { describe, expect, it, vi, beforeEach } from 'vitest';
import { render } from 'vitest-browser-svelte';
import RegisterPage from './+page.svelte';
import { client } from '$lib/api/client';

vi.mock('$app/state', () => ({
	page: {
		url: new URL('http://localhost:3000/register')
	}
}));

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

describe('RegisterPage', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('renders registration form fields', async () => {
		const screen = render(RegisterPage);

		expect(screen.getByRole('button', { name: /Sign up|Daftar/i })).toBeDefined();
		expect(screen.getByLabelText(/Full Name|Nama Lengkap/i)).toBeDefined();
		expect(screen.getByLabelText(/Email/i)).toBeDefined();
		expect(screen.getByLabelText(/Password/i)).toBeDefined();
		expect(screen.getByLabelText(/Organization Name|Nama Organisasi/i)).toBeDefined();
	});

	it('handles 409 email conflict error', async () => {
		const screen = render(RegisterPage);

		vi.spyOn(client, 'POST').mockResolvedValue({
			error: {
				error: {
					code: 'CONFLICT',
					message: 'Email already exists'
				}
			},
			response: new Response(
				JSON.stringify({
					error: {
						code: 'CONFLICT',
						message: 'Email already exists'
					}
				}),
				{ status: 409 }
			)
		} as never);

		const nameInput = screen.getByLabelText(/Full Name|Nama Lengkap/i);
		const emailInput = screen.getByLabelText(/Email/i);
		const passwordInput = screen.getByLabelText(/Password/i);
		const orgInput = screen.getByLabelText(/Organization Name|Nama Organisasi/i);
		const submitBtn = screen.getByRole('button', { name: /Sign up|Daftar/i });

		await nameInput.fill('John Doe');
		await emailInput.fill('exists@example.com');
		await passwordInput.fill('securePassword123');
		await orgInput.fill('My Company');
		await submitBtn.click();

		expect(screen.getByText(/Email sudah terdaftar|Email is already registered/i)).toBeDefined();
	});
});
