import { describe, expect, it } from 'vitest';
import idMessages from '../../../messages/id.json';
import enMessages from '../../../messages/en.json';

describe('i18n message key parity', () => {
	it('has identical keys in id.json and en.json', () => {
		const idKeys = Object.keys(idMessages).sort();
		const enKeys = Object.keys(enMessages).sort();

		const missingInEn = idKeys.filter((key) => !Object.hasOwn(enMessages, key));
		const missingInId = enKeys.filter((key) => !Object.hasOwn(idMessages, key));

		expect(
			missingInEn,
			`Keys present in id.json but missing in en.json: ${missingInEn.join(', ')}`
		).toEqual([]);

		expect(
			missingInId,
			`Keys present in en.json but missing in id.json: ${missingInId.join(', ')}`
		).toEqual([]);

		expect(idKeys).toEqual(enKeys);
	});
});
