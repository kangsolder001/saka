import { intlLocale } from '$lib/i18n';

/**
 * Format a numeric amount as currency string without decimals.
 * Never divide IDR amounts by 100 per spec §8.1.
 */
export function formatMoney(amount: number, currency: string = 'IDR', locale?: string): string {
	const loc = intlLocale(locale);
	return new Intl.NumberFormat(loc, {
		style: 'currency',
		currency,
		maximumFractionDigits: 0,
		minimumFractionDigits: 0
	}).format(amount);
}
