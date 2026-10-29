import { describe, expect, it } from 'vitest';
import { formatMoney } from './money';

describe('formatMoney', () => {
	it('formats 99000 IDR in id-ID without decimals', () => {
		const formatted = formatMoney(99000, 'IDR', 'id-ID');
		// Should contain 99.000 (with dot thousand separator) and not contain decimals
		expect(formatted).toContain('99.000');
		expect(formatted).not.toContain(',00');
		expect(formatted).not.toMatch(/,00$/);
		expect(formatted).toMatch(/^Rp/);
	});

	it('formats 99000 IDR with short locale "id"', () => {
		const formatted = formatMoney(99000, 'IDR', 'id');
		expect(formatted).toContain('99.000');
	});

	it('formats 99000 IDR in en-US', () => {
		const formatted = formatMoney(99000, 'IDR', 'en-US');
		expect(formatted).toContain('99,000');
		expect(formatted).not.toContain('.00');
	});

	it('defaults to IDR currency when currency is omitted', () => {
		const formatted = formatMoney(150000, undefined, 'id-ID');
		expect(formatted).toContain('150.000');
		expect(formatted).toMatch(/^Rp/);
	});

	it('formats zero correctly', () => {
		const formatted = formatMoney(0, 'IDR', 'id-ID');
		expect(formatted).toContain('0');
		expect(formatted).not.toContain(',00');
	});

	it('formats other currencies without decimals per maximumFractionDigits: 0', () => {
		const formatted = formatMoney(50, 'USD', 'en-US');
		expect(formatted).toContain('50');
		expect(formatted).not.toContain('.00');
	});
});
