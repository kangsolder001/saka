import { describe, expect, it } from 'vitest';
import { formatDate, formatDateTime, formatRelative } from './date';

describe('formatDate', () => {
	it('formats ISO string in id-ID', () => {
		const formatted = formatDate('2026-10-29T12:00:00Z', 'id-ID', {
			timeZone: 'UTC'
		});
		expect(formatted).toBe('29 Okt 2026');
	});

	it('formats ISO string in en-US', () => {
		const formatted = formatDate('2026-10-29T12:00:00Z', 'en-US', {
			timeZone: 'UTC'
		});
		expect(formatted).toBe('Oct 29, 2026');
	});

	it('handles null, undefined, and invalid date strings', () => {
		expect(formatDate(null)).toBe('');
		expect(formatDate(undefined)).toBe('');
		expect(formatDate('invalid-date')).toBe('');
	});
});

describe('formatDateTime', () => {
	it('formats ISO string with date and time in id-ID', () => {
		const formatted = formatDateTime('2026-10-29T14:30:00Z', 'id-ID', {
			timeZone: 'UTC'
		});
		expect(formatted).toContain('29 Okt 2026');
		expect(formatted).toContain('14.30');
	});

	it('formats ISO string with date and time in en-US', () => {
		const formatted = formatDateTime('2026-10-29T14:30:00Z', 'en-US', {
			timeZone: 'UTC'
		});
		expect(formatted).toContain('Oct 29, 2026');
		expect(formatted).toContain('02:30 PM');
	});

	it('handles invalid inputs gracefully', () => {
		expect(formatDateTime(null)).toBe('');
		expect(formatDateTime(undefined)).toBe('');
		expect(formatDateTime('invalid')).toBe('');
	});
});

describe('formatRelative', () => {
	const base = new Date('2026-10-29T12:00:00Z');

	it('formats future days as "dalam 3 hari" and "in 3 days"', () => {
		const target = new Date('2026-11-01T12:00:00Z'); // 3 days later
		expect(formatRelative(target, base, 'id-ID')).toBe('dalam 3 hari');
		expect(formatRelative(target, base, 'en-US')).toBe('in 3 days');
	});

	it('formats past days as "3 hari yang lalu" and "3 days ago"', () => {
		const target = new Date('2026-10-26T12:00:00Z'); // 3 days ago
		expect(formatRelative(target, base, 'id-ID')).toBe('3 hari yang lalu');
		expect(formatRelative(target, base, 'en-US')).toBe('3 days ago');
	});

	it('formats hours difference', () => {
		const target = new Date('2026-10-29T14:00:00Z'); // 2 hours later
		expect(formatRelative(target, base, 'id-ID')).toBe('dalam 2 jam');
		expect(formatRelative(target, base, 'en-US')).toBe('in 2 hours');
	});

	it('formats minutes difference', () => {
		const target = new Date('2026-10-29T11:50:00Z'); // 10 minutes ago
		expect(formatRelative(target, base, 'id-ID')).toBe('10 menit yang lalu');
		expect(formatRelative(target, base, 'en-US')).toBe('10 minutes ago');
	});

	it('formats near zero difference as "sekarang" / "now"', () => {
		expect(formatRelative(base, base, 'id-ID')).toBe('sekarang');
		expect(formatRelative(base, base, 'en-US')).toBe('now');
	});

	it('handles invalid dates', () => {
		expect(formatRelative(null, base)).toBe('');
		expect(formatRelative('invalid', base)).toBe('');
	});
});
