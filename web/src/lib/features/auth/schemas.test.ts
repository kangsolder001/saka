import { describe, expect, it } from 'vitest';
import { forgotPasswordSchema, loginSchema, registerSchema, resetPasswordSchema } from './schemas';

describe('auth schemas', () => {
	describe('loginSchema', () => {
		it('accepts valid email and password', () => {
			const res = loginSchema.safeParse({
				email: 'user@example.com',
				password: 'password123'
			});
			expect(res.success).toBe(true);
		});

		it('rejects invalid email', () => {
			const res = loginSchema.safeParse({
				email: 'not-an-email',
				password: 'password123'
			});
			expect(res.success).toBe(false);
		});

		it('rejects empty password', () => {
			const res = loginSchema.safeParse({
				email: 'user@example.com',
				password: ''
			});
			expect(res.success).toBe(false);
		});
	});

	describe('registerSchema', () => {
		it('accepts valid registration input', () => {
			const res = registerSchema.safeParse({
				name: 'John Doe',
				email: 'john@example.com',
				password: 'securePassword123',
				organization_name: 'Acme Corp'
			});
			expect(res.success).toBe(true);
		});

		it('rejects password same as email', () => {
			const res = registerSchema.safeParse({
				name: 'John Doe',
				email: 'john@example.com',
				password: 'john@example.com',
				organization_name: 'Acme Corp'
			});
			expect(res.success).toBe(false);
			if (!res.success) {
				expect(res.error.issues[0]?.path).toContain('password');
			}
		});

		it('rejects password under 8 characters', () => {
			const res = registerSchema.safeParse({
				name: 'John Doe',
				email: 'john@example.com',
				password: 'short',
				organization_name: 'Acme Corp'
			});
			expect(res.success).toBe(false);
		});
	});

	describe('forgotPasswordSchema', () => {
		it('accepts valid email', () => {
			const res = forgotPasswordSchema.safeParse({
				email: 'user@example.com'
			});
			expect(res.success).toBe(true);
		});

		it('rejects invalid email', () => {
			const res = forgotPasswordSchema.safeParse({
				email: 'invalid-email'
			});
			expect(res.success).toBe(false);
		});
	});

	describe('resetPasswordSchema', () => {
		it('accepts matching passwords', () => {
			const res = resetPasswordSchema.safeParse({
				password: 'newPassword123',
				confirmPassword: 'newPassword123'
			});
			expect(res.success).toBe(true);
		});

		it('rejects mismatching passwords', () => {
			const res = resetPasswordSchema.safeParse({
				password: 'newPassword123',
				confirmPassword: 'differentPassword123'
			});
			expect(res.success).toBe(false);
			if (!res.success) {
				expect(res.error.issues[0]?.path).toContain('confirmPassword');
			}
		});
	});
});
