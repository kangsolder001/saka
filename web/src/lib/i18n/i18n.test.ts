import { describe, it, expect } from 'vitest';
import { getLocale, setLocale, locales, intlLocale } from './index';
import * as m from '$lib/paraglide/messages';

describe('i18n', () => {
	it('exports valid locales list', () => {
		expect(locales).toContain('id');
		expect(locales).toContain('en');
	});

	it('maps locale to full intl locale code', () => {
		expect(intlLocale('id')).toBe('id-ID');
		expect(intlLocale('en')).toBe('en-US');
		expect(intlLocale('fr')).toBe('fr');
	});

	it('resolves message strings correctly', () => {
		setLocale('id');
		expect(getLocale()).toBe('id');
		expect(m.common_save()).toBe('Simpan');
		expect(m.common_cancel()).toBe('Batal');

		setLocale('en');
		expect(getLocale()).toBe('en');
		expect(m.common_save()).toBe('Save');
		expect(m.common_cancel()).toBe('Cancel');
	});
});
