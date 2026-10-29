import { beforeEach, describe, expect, it } from 'vitest';
import { ApiError } from '$lib/api/errors';
import {
	closeUpgradeDialog,
	openUpgradeDialog,
	showUpgrade,
	upgradeDialog
} from './upgrade.svelte';

describe('billing upgrade store', () => {
	beforeEach(() => {
		closeUpgradeDialog();
	});

	it('showUpgrade triggers on PLAN_LIMIT_REACHED', () => {
		const err = new ApiError({
			status: 402,
			code: 'PLAN_LIMIT_REACHED',
			serverMessage: 'Limit reached',
			details: {}
		});

		const handled = showUpgrade(err, 'org-123');
		expect(handled).toBe(true);
		expect(upgradeDialog.open).toBe(true);
		expect(upgradeDialog.orgId).toBe('org-123');
	});

	it('showUpgrade triggers on PLAN_FEATURE_UNAVAILABLE', () => {
		const err = new ApiError({
			status: 402,
			code: 'PLAN_FEATURE_UNAVAILABLE',
			serverMessage: 'Feature unavailable',
			details: {}
		});

		const handled = showUpgrade(err, 'org-456');
		expect(handled).toBe(true);
		expect(upgradeDialog.open).toBe(true);
		expect(upgradeDialog.orgId).toBe('org-456');
	});

	it('showUpgrade returns false for other errors', () => {
		const validationErr = new ApiError({
			status: 400,
			code: 'VALIDATION_ERROR',
			serverMessage: 'Bad request',
			details: {}
		});
		expect(showUpgrade(validationErr, 'org-123')).toBe(false);
		expect(upgradeDialog.open).toBe(false);

		const generalErr = new Error('Random error');
		expect(showUpgrade(generalErr, 'org-123')).toBe(false);
		expect(upgradeDialog.open).toBe(false);
	});

	it('openUpgradeDialog and closeUpgradeDialog manage state', () => {
		openUpgradeDialog('org-99');
		expect(upgradeDialog.open).toBe(true);
		expect(upgradeDialog.orgId).toBe('org-99');

		closeUpgradeDialog();
		expect(upgradeDialog.open).toBe(false);
		expect(upgradeDialog.orgId).toBe('');
	});
});
