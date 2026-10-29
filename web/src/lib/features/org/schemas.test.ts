import { describe, it, expect } from 'vitest';
import { createOrgSchema, updateOrgSchema, transferOwnershipSchema } from './schemas';

describe('org schemas (§9.2)', () => {
	describe('createOrgSchema', () => {
		it('accepts valid name without slug', () => {
			const res = createOrgSchema.safeParse({ name: 'Acme Corp' });
			expect(res.success).toBe(true);
		});

		it('accepts valid name and valid slug', () => {
			const res = createOrgSchema.safeParse({ name: 'Acme Corp', slug: 'acme-corp-123' });
			expect(res.success).toBe(true);
		});

		it('rejects empty name', () => {
			const res = createOrgSchema.safeParse({ name: '   ' });
			expect(res.success).toBe(false);
		});

		it('rejects invalid slug format', () => {
			expect(createOrgSchema.safeParse({ name: 'Acme', slug: 'Acme' }).success).toBe(false);
			expect(createOrgSchema.safeParse({ name: 'Acme', slug: 'acme--corp' }).success).toBe(false);
			expect(createOrgSchema.safeParse({ name: 'Acme', slug: '-acme' }).success).toBe(false);
			expect(createOrgSchema.safeParse({ name: 'Acme', slug: 'acme-' }).success).toBe(false);
			expect(createOrgSchema.safeParse({ name: 'Acme', slug: 'ab' }).success).toBe(false); // < 3 chars
		});
	});

	describe('updateOrgSchema', () => {
		it('accepts name update only', () => {
			const res = updateOrgSchema.safeParse({ name: 'New Name' });
			expect(res.success).toBe(true);
		});

		it('accepts valid logo_url http/https', () => {
			expect(updateOrgSchema.safeParse({ logo_url: 'https://example.com/logo.png' }).success).toBe(
				true
			);
			expect(updateOrgSchema.safeParse({ logo_url: 'http://example.com/logo.png' }).success).toBe(
				true
			);
			expect(updateOrgSchema.safeParse({ logo_url: 'ftp://example.com/logo.png' }).success).toBe(
				false
			);
			expect(updateOrgSchema.safeParse({ logo_url: 'invalid-url' }).success).toBe(false);
		});

		it('rejects empty object with no fields', () => {
			const res = updateOrgSchema.safeParse({});
			expect(res.success).toBe(false);
		});
	});

	describe('transferOwnershipSchema', () => {
		it('accepts valid uuid and password', () => {
			const res = transferOwnershipSchema.safeParse({
				user_id: '550e8400-e29b-41d4-a716-446655440000',
				current_password: 'secretpassword123'
			});
			expect(res.success).toBe(true);
		});

		it('rejects invalid uuid', () => {
			const res = transferOwnershipSchema.safeParse({
				user_id: 'not-a-uuid',
				current_password: 'secretpassword123'
			});
			expect(res.success).toBe(false);
		});
	});
});
