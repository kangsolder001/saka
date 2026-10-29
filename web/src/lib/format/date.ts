import { intlLocale } from '$lib/i18n';

/**
 * Format a date as localized short date string (e.g. "29 Okt 2026" in id-ID, "Oct 29, 2026" in en-US).
 */
export function formatDate(
	date: string | number | Date | null | undefined,
	locale?: string,
	options?: Intl.DateTimeFormatOptions
): string {
	if (date == null) return '';
	const d = date instanceof Date ? date : new Date(date);
	if (isNaN(d.getTime())) return '';

	const loc = intlLocale(locale);
	return new Intl.DateTimeFormat(loc, {
		day: 'numeric',
		month: 'short',
		year: 'numeric',
		...options
	}).format(d);
}

/**
 * Format a date and time as localized string (e.g. "29 Okt 2026, 14.30" in id-ID).
 */
export function formatDateTime(
	date: string | number | Date | null | undefined,
	locale?: string,
	options?: Intl.DateTimeFormatOptions
): string {
	if (date == null) return '';
	const d = date instanceof Date ? date : new Date(date);
	if (isNaN(d.getTime())) return '';

	const loc = intlLocale(locale);
	return new Intl.DateTimeFormat(loc, {
		day: 'numeric',
		month: 'short',
		year: 'numeric',
		hour: '2-digit',
		minute: '2-digit',
		...options
	}).format(d);
}

/**
 * Format relative time using Intl.RelativeTimeFormat (days, hours, minutes).
 * e.g. "dalam 3 hari" / "in 3 days", "3 hari yang lalu" / "3 days ago".
 */
export function formatRelative(
	date: string | number | Date | null | undefined,
	now: string | number | Date = new Date(),
	locale?: string
): string {
	if (date == null) return '';
	const target = date instanceof Date ? date : new Date(date);
	const base = now instanceof Date ? now : new Date(now);

	if (isNaN(target.getTime()) || isNaN(base.getTime())) {
		return '';
	}

	const diffMs = target.getTime() - base.getTime();
	const diffSec = Math.round(diffMs / 1000);
	const diffMin = Math.round(diffMs / (60 * 1000));
	const diffHours = Math.round(diffMs / (3600 * 1000));
	const diffDays = Math.round(diffMs / (86400 * 1000));

	const loc = intlLocale(locale);
	const rtf = new Intl.RelativeTimeFormat(loc, { numeric: 'auto' });

	const absSec = Math.abs(diffSec);
	const absMin = Math.abs(diffMin);
	const absHours = Math.abs(diffHours);

	if (absSec < 45) {
		return rtf.format(diffSec, 'second');
	}
	if (absMin < 60) {
		return rtf.format(diffMin, 'minute');
	}
	if (absHours < 24) {
		return rtf.format(diffHours, 'hour');
	}
	return rtf.format(diffDays, 'day');
}
