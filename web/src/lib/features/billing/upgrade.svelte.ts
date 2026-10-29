import { ApiError } from '$lib/api/errors';

export const upgradeDialog = $state<{
	open: boolean;
	orgId: string;
}>({
	open: false,
	orgId: ''
});

export function openUpgradeDialog(orgId: string): void {
	upgradeDialog.open = true;
	upgradeDialog.orgId = orgId;
}

export function closeUpgradeDialog(): void {
	upgradeDialog.open = false;
	upgradeDialog.orgId = '';
}

export function showUpgrade(err: unknown, orgId: string): boolean {
	if (
		err instanceof ApiError &&
		(err.code === 'PLAN_LIMIT_REACHED' || err.code === 'PLAN_FEATURE_UNAVAILABLE')
	) {
		openUpgradeDialog(orgId);
		return true;
	}
	return false;
}
