import { describe, expect, it } from 'vitest';
import {
	profileSchema,
	changePasswordSchema,
	changeEmailSchema,
	deleteAccountSchema
} from './schemas';

describe('account schemas', () => {
	describe('profileSchema', () => {
		it('validates valid profile data', () => {
			const res = profileSchema.safeParse({
				name: 'John Doe',
				avatar_url: 'https://example.com/pic.png'
			});
			expect(res.success).toBe(true);
		});

		it('accepts null or empty avatar_url', () => {
			expect(profileSchema.safeParse({ name: 'John', avatar_url: null }).success).toBe(true);
			expect(profileSchema.safeParse({ name: 'John', avatar_url: '' }).success).toBe(true);
		});

		it('rejects invalid avatar_url scheme', () => {
			const res = profileSchema.safeParse({
				name: 'John',
				avatar_url: 'javascript:alert(1)'
			});
			expect(res.success).toBe(false);
		});

		it('rejects empty name', () => {
			expect(profileSchema.safeParse({ name: '   ' }).success).toBe(false);
		});
	});

	describe('changePasswordSchema', () => {
		it('validates matching passwords with valid length', () => {
			const res = changePasswordSchema.safeParse({
				current_password: 'oldPassword1',
				new_password: 'newPassword1',
				confirm_password: 'newPassword1'
			});
			expect(res.success).toBe(true);
		});

		it('rejects passwords shorter than 8 characters', () => {
			const res = changePasswordSchema.safeParse({
				current_password: 'oldPassword1',
				new_password: 'short',
				confirm_password: 'short'
			});
			expect(res.success).toBe(false);
		});

		it('rejects mismatched passwords', () => {
			const res = changePasswordSchema.safeParse({
				current_password: 'oldPassword1',
				new_password: 'newPassword1',
				confirm_password: 'differentPassword2'
			});
			expect(res.success).toBe(false);
		});
	});

	describe('changeEmailSchema', () => {
		it('validates valid new email and password', () => {
			const res = changeEmailSchema.safeParse({
				new_email: 'new@example.com',
				current_password: 'password123'
			});
			expect(res.success).toBe(true);
		});

		it('rejects invalid email', () => {
			const res = changeEmailSchema.safeParse({
				new_email: 'invalid-email',
				current_password: 'password123'
			});
			expect(res.success).toBe(false);
		});
	});

	describe('deleteAccountSchema', () => {
		it('validates required password', () => {
			expect(deleteAccountSchema.safeParse({ current_password: 'pass' }).success).toBe(true);
			expect(deleteAccountSchema.safeParse({ current_password: '' }).success).toBe(false);
		});
	});
});
