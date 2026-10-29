export interface DowngradeWarning {
	limit: string;
	used: number;
	max: number;
}

export const scheduledDowngradeDialog = $state<{
	open: boolean;
	pendingPlan: string;
	currentPeriodEnd: string | null;
	warnings: DowngradeWarning[];
}>({
	open: false,
	pendingPlan: '',
	currentPeriodEnd: null,
	warnings: []
});

export function openScheduledDowngradeDialog(params: {
	pendingPlan: string;
	currentPeriodEnd: string | null;
	warnings?: DowngradeWarning[];
}): void {
	scheduledDowngradeDialog.open = true;
	scheduledDowngradeDialog.pendingPlan = params.pendingPlan;
	scheduledDowngradeDialog.currentPeriodEnd = params.currentPeriodEnd;
	scheduledDowngradeDialog.warnings = params.warnings || [];
}

export function closeScheduledDowngradeDialog(): void {
	scheduledDowngradeDialog.open = false;
	scheduledDowngradeDialog.pendingPlan = '';
	scheduledDowngradeDialog.currentPeriodEnd = null;
	scheduledDowngradeDialog.warnings = [];
}
