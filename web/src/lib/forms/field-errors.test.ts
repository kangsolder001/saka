import { describe, it, expect, beforeEach } from 'vitest';
import { fieldErrorMessage } from './field-errors';
import { setLocale } from '$lib/i18n';

describe('fieldErrorMessage', () => {
	beforeEach(() => {
		setLocale('id');
	});

	it('translates all table pairs in Indonesian', () => {
		expect(fieldErrorMessage('current_password', 'is incorrect')).toBe('Password saat ini salah');
		expect(fieldErrorMessage('new_password', 'must be different from the current password')).toBe(
			'Password baru harus berbeda dari password saat ini'
		);
		expect(fieldErrorMessage('new_email', 'must be different from the current email')).toBe(
			'Email baru sama dengan email saat ini'
		);
		expect(fieldErrorMessage('token', 'invalid or expired')).toBe(
			'Tautan tidak valid atau sudah kedaluwarsa'
		);
		expect(fieldErrorMessage('user_id', 'must be another member')).toBe('Pilih anggota lain');
		expect(fieldErrorMessage('plan', 'is not a known plan')).toBe('Plan tidak dikenal');
		expect(fieldErrorMessage('any_field', 'is required')).toBe('Wajib diisi');
	});

	it('translates all table pairs in English', () => {
		setLocale('en');

		expect(fieldErrorMessage('current_password', 'is incorrect')).toBe(
			'Current password is incorrect'
		);
		expect(fieldErrorMessage('new_password', 'must be different from the current password')).toBe(
			'New password must be different from current password'
		);
		expect(fieldErrorMessage('new_email', 'must be different from the current email')).toBe(
			'New email must be different from current email'
		);
		expect(fieldErrorMessage('token', 'invalid or expired')).toBe('Link is invalid or expired');
		expect(fieldErrorMessage('user_id', 'must be another member')).toBe(
			'Please select another member'
		);
		expect(fieldErrorMessage('plan', 'is not a known plan')).toBe('Unknown plan');
		expect(fieldErrorMessage('any_field', 'is required')).toBe('This field is required');
	});

	it('falls back to generic invalid value message for unknown reasons', () => {
		setLocale('id');
		expect(fieldErrorMessage('name', 'some weird error')).toBe('Nilai tidak valid');

		setLocale('en');
		expect(fieldErrorMessage('name', 'some weird error')).toBe('Invalid value');
	});
});
