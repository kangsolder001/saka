import { beforeEach, describe, expect, it } from 'vitest';
import { clearLastOrgId, getLastOrgId, LAST_ORG_ID_KEY, setLastOrgId } from './api';

describe('lastOrgId storage helpers', () => {
	beforeEach(() => {
		localStorage.clear();
	});

	it('returns null when key is not set', () => {
		expect(getLastOrgId()).toBeNull();
	});

	it('sets and gets last org id', () => {
		setLastOrgId('org-abc-123');
		expect(localStorage.getItem(LAST_ORG_ID_KEY)).toBe('org-abc-123');
		expect(getLastOrgId()).toBe('org-abc-123');
	});

	it('clears last org id', () => {
		setLastOrgId('org-abc-123');
		clearLastOrgId();
		expect(getLastOrgId()).toBeNull();
		expect(localStorage.getItem(LAST_ORG_ID_KEY)).toBeNull();
	});

	it('handles null passed to setLastOrgId', () => {
		setLastOrgId('org-xyz');
		setLastOrgId(null);
		expect(getLastOrgId()).toBeNull();
	});
});
