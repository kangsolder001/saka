import {
	getLocale as paraglideGetLocale,
	setLocale as paraglideSetLocale,
	locales,
	type Locale
} from '$lib/paraglide/runtime.js';

export { locales, type Locale };

export function getLocale(): Locale {
	return paraglideGetLocale();
}

export function setLocale(newLocale: Locale, options?: { reload?: boolean }): void {
	paraglideSetLocale(newLocale, { reload: false, ...options });
}

export function intlLocale(loc: string = getLocale()): string {
	if (loc === 'id') return 'id-ID';
	if (loc === 'en') return 'en-US';
	return loc;
}
