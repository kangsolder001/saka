import { describe, it, expect, beforeEach } from 'vitest';
import { errorMessage } from './error-message';
import { ApiError } from './errors';
import { setLocale } from '$lib/i18n';
import type { ErrorCode } from '$lib/api/types';

describe('errorMessage', () => {
	beforeEach(() => {
		setLocale('id');
	});

	it('translates specific known reasons in Indonesian', () => {
		const err = new ApiError({
			status: 403,
			code: 'FORBIDDEN',
			serverMessage: 'owner must transfer',
			details: { reason: 'owner_must_transfer_ownership' }
		});

		expect(errorMessage(err)).toBe('Owner harus memindahkan kepemilikan sebelum keluar.');
	});

	it('translates specific known reasons in English', () => {
		setLocale('en');
		const err = new ApiError({
			status: 403,
			code: 'FORBIDDEN',
			serverMessage: 'owner must transfer',
			details: { reason: 'owner_must_transfer_ownership' }
		});

		expect(errorMessage(err)).toBe('The owner must transfer ownership before leaving.');
	});

	it('falls back to code message when reason is unknown', () => {
		const err = new ApiError({
			status: 403,
			code: 'FORBIDDEN',
			serverMessage: 'Forbidden access',
			details: { reason: 'some_random_reason' }
		});

		expect(errorMessage(err)).toBe('Anda tidak memiliki akses.');
	});

	it('translates all standard error codes in Indonesian and English', () => {
		const codes: ErrorCode[] = [
			'VALIDATION_ERROR',
			'UNAUTHENTICATED',
			'TOKEN_EXPIRED',
			'FORBIDDEN',
			'EMAIL_NOT_VERIFIED',
			'PLAN_LIMIT_REACHED',
			'PLAN_FEATURE_UNAVAILABLE',
			'SUBSCRIPTION_INACTIVE',
			'NOT_FOUND',
			'CONFLICT',
			'PAYMENT_PROVIDER_ERROR',
			'INTERNAL_ERROR'
		];

		for (const code of codes) {
			setLocale('id');
			const err = new ApiError({
				status: 400,
				code,
				serverMessage: 'Server detail',
				details: {}
			});
			const idMsg = errorMessage(err);
			expect(idMsg).toBeTruthy();
			expect(idMsg).not.toBe('Server detail');

			setLocale('en');
			const enMsg = errorMessage(err);
			expect(enMsg).toBeTruthy();
			expect(enMsg).not.toBe('Server detail');
			expect(enMsg).not.toBe(idMsg);
		}
	});

	it('translates NETWORK_ERROR correctly', () => {
		setLocale('id');
		const err = new ApiError({
			status: 0,
			code: 'NETWORK_ERROR',
			serverMessage: 'Failed to fetch',
			details: {}
		});
		expect(errorMessage(err)).toBe('Tidak dapat terhubung ke server.');

		setLocale('en');
		expect(errorMessage(err)).toBe('Unable to connect to the server.');
	});

	it('handles RATE_LIMITED with seconds (< 60s)', () => {
		setLocale('id');
		const err = new ApiError({
			status: 429,
			code: 'RATE_LIMITED',
			serverMessage: 'Rate limited',
			details: { retry_after_seconds: 30 },
			retryAfterSeconds: 30
		});
		expect(errorMessage(err)).toBe('Terlalu banyak permintaan. Coba lagi dalam 30 detik.');

		setLocale('en');
		expect(errorMessage(err)).toBe('Too many requests. Try again in 30 seconds.');
	});

	it('handles RATE_LIMITED with minutes (> 60s)', () => {
		setLocale('id');
		const err = new ApiError({
			status: 429,
			code: 'RATE_LIMITED',
			serverMessage: 'Rate limited',
			details: { retry_after_seconds: 120 },
			retryAfterSeconds: 120
		});
		expect(errorMessage(err)).toBe('Terlalu banyak permintaan. Coba lagi dalam 2 menit.');

		setLocale('en');
		expect(errorMessage(err)).toBe('Too many requests. Try again in 2 minutes.');
	});

	it('falls back to serverMessage when code is unknown', () => {
		const err = new ApiError({
			status: 500,
			code: 'CUSTOM_UNKNOWN_CODE' as unknown as ErrorCode,
			serverMessage: 'Custom technical error detail',
			details: {}
		});
		expect(errorMessage(err)).toBe('Custom technical error detail');
	});

	it('falls back to error_internal_error when code is unknown and serverMessage is empty', () => {
		const err = new ApiError({
			status: 500,
			code: 'CUSTOM_UNKNOWN_CODE' as unknown as ErrorCode,
			serverMessage: '',
			details: {}
		});
		expect(errorMessage(err)).toBe('Terjadi kesalahan pada server.');
	});

	it('handles non-ApiError instances', () => {
		expect(errorMessage(new Error('Simple standard error'))).toBe('Simple standard error');
		expect(errorMessage('unknown string')).toBe('Terjadi kesalahan pada server.');
	});
});
