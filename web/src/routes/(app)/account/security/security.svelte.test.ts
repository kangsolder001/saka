import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';
import SecurityPage from './+page.svelte';
import { client } from '$lib/api/client';

describe('account security page', () => {
	beforeEach(() => {
		vi.restoreAllMocks();
	});

	it('submits change password form successfully', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { message: 'Password changed' },
			response: new Response(JSON.stringify({ message: 'Password changed' }), { status: 200 })
		} as never);

		render(SecurityPage);

		const currentPassInput = document.querySelector<HTMLInputElement>('#current_password')!;
		const newPassInput = document.querySelector<HTMLInputElement>('#new_password')!;
		const confirmPassInput = document.querySelector<HTMLInputElement>('#confirm_password')!;
		const submitBtn = Array.from(
			document.querySelectorAll<HTMLButtonElement>('button[type="submit"]')
		)[0];

		currentPassInput.value = 'currentPassword123';
		currentPassInput.dispatchEvent(new Event('input'));
		newPassInput.value = 'newPassword456';
		newPassInput.dispatchEvent(new Event('input'));
		confirmPassInput.value = 'newPassword456';
		confirmPassInput.dispatchEvent(new Event('input'));

		submitBtn?.click();

		await vi.waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/me/password', {
				body: {
					current_password: 'currentPassword123',
					new_password: 'newPassword456'
				}
			});
		});
	});

	it('submits change email form successfully', async () => {
		const postSpy = vi.spyOn(client, 'POST').mockResolvedValue({
			data: { message: 'Verification sent' },
			response: new Response(JSON.stringify({ message: 'Verification sent' }), { status: 200 })
		} as never);

		render(SecurityPage);

		const newEmailInput = document.querySelector<HTMLInputElement>('#new_email')!;
		const emailPassInput = document.querySelector<HTMLInputElement>('#email_current_password')!;
		const submitBtn = Array.from(
			document.querySelectorAll<HTMLButtonElement>('button[type="submit"]')
		)[1];

		newEmailInput.value = 'fresh@example.com';
		newEmailInput.dispatchEvent(new Event('input'));
		emailPassInput.value = 'mySecretPassword';
		emailPassInput.dispatchEvent(new Event('input'));

		submitBtn?.click();

		await vi.waitFor(() => {
			expect(postSpy).toHaveBeenCalledWith('/me/email', {
				body: {
					new_email: 'fresh@example.com',
					current_password: 'mySecretPassword'
				}
			});
		});
	});
});
