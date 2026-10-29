import { describe, it, expect, vi, beforeEach } from 'vitest';
import { z } from 'zod';
import { createForm } from './form.svelte';
import { ApiError } from '$lib/api/errors';
import { setLocale } from '$lib/i18n';

describe('createForm', () => {
	beforeEach(() => {
		setLocale('id');
		document.body.innerHTML = '';
	});

	const testSchema = z.object({
		email: z.string().email('Format email tidak valid'),
		password: z.string().min(8, 'Password minimal 8 karakter')
	});

	it('does NOT call onSubmit when Zod client validation fails and focuses first error', async () => {
		const emailInput = document.createElement('input');
		emailInput.name = 'email';
		document.body.appendChild(emailInput);
		const focusSpy = vi.spyOn(emailInput, 'focus');

		const onSubmit = vi.fn();
		const form = createForm({
			schema: testSchema,
			initial: { email: 'invalid-email', password: '' },
			onSubmit
		});

		const success = await form.submit();

		expect(success).toBe(false);
		expect(onSubmit).not.toHaveBeenCalled();
		expect(form.errors.email).toBe('Format email tidak valid');
		expect(form.errors.password).toBe('Password minimal 8 karakter');
		expect(focusSpy).toHaveBeenCalledTimes(1);
	});

	it('calls onSubmit when valid and handles submitting state', async () => {
		let wasSubmittingDuringCall = false;
		const onSubmit = vi.fn().mockImplementation(async () => {
			wasSubmittingDuringCall = form.submitting;
		});

		const form = createForm({
			schema: testSchema,
			initial: { email: 'valid@example.com', password: 'password123' },
			onSubmit
		});

		const success = await form.submit();

		expect(success).toBe(true);
		expect(onSubmit).toHaveBeenCalledTimes(1);
		expect(onSubmit).toHaveBeenCalledWith({
			email: 'valid@example.com',
			password: 'password123'
		});
		expect(wasSubmittingDuringCall).toBe(true);
		expect(form.submitting).toBe(false);
		expect(Object.keys(form.errors)).toHaveLength(0);
		expect(form.formError).toBeNull();
	});

	it('maps backend VALIDATION_ERROR to field errors', async () => {
		const onSubmit = vi.fn().mockRejectedValue(
			new ApiError({
				status: 400,
				code: 'VALIDATION_ERROR',
				serverMessage: 'Validation failed',
				details: {
					fields: {
						email: 'is required'
					}
				}
			})
		);

		const form = createForm({
			schema: testSchema,
			initial: { email: 'valid@example.com', password: 'password123' },
			onSubmit
		});

		const success = await form.submit();

		expect(success).toBe(false);
		expect(form.errors.email).toBe('Wajib diisi');
		expect(form.formError).toBeNull();
		expect(form.submitting).toBe(false);
	});

	it('maps backend VALIDATION_ERROR on unknown field to formError', async () => {
		const onSubmit = vi.fn().mockRejectedValue(
			new ApiError({
				status: 400,
				code: 'VALIDATION_ERROR',
				serverMessage: 'Validation failed',
				details: {
					fields: {
						body: 'is required'
					}
				}
			})
		);

		const form = createForm({
			schema: testSchema,
			initial: { email: 'valid@example.com', password: 'password123' },
			onSubmit
		});

		const success = await form.submit();

		expect(success).toBe(false);
		expect(form.formError).toBe('Wajib diisi');
		expect(form.submitting).toBe(false);
	});

	it('sets formError for other ApiErrors', async () => {
		const onSubmit = vi.fn().mockRejectedValue(
			new ApiError({
				status: 403,
				code: 'FORBIDDEN',
				serverMessage: 'Forbidden',
				details: { reason: 'owner_must_transfer_ownership' }
			})
		);

		const form = createForm({
			schema: testSchema,
			initial: { email: 'valid@example.com', password: 'password123' },
			onSubmit
		});

		const success = await form.submit();

		expect(success).toBe(false);
		expect(form.formError).toBe('Owner harus memindahkan kepemilikan sebelum keluar.');
		expect(form.submitting).toBe(false);
	});

	it('resets form values and errors', () => {
		const form = createForm({
			schema: testSchema,
			initial: { email: 'initial@example.com', password: 'initial-password' },
			onSubmit: vi.fn()
		});

		form.values.email = 'modified@example.com';
		form.errors.email = 'Some error';
		form.formError = 'Form level error';

		form.reset();

		expect(form.values.email).toBe('initial@example.com');
		expect(form.errors.email).toBeUndefined();
		expect(form.formError).toBeNull();
	});
});
