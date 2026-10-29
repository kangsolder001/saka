<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { formatDate } from '$lib/format/date';
	import {
		scheduledDowngradeDialog,
		closeScheduledDowngradeDialog
	} from '../downgrade-dialog.svelte';
	import * as m from '$lib/paraglide/messages.js';

	function getLimitName(key: string): string {
		const messageKey = `billing_limit_${key}` as keyof typeof m;
		if (typeof m[messageKey] === 'function') {
			return (m[messageKey] as () => string)();
		}
		return key;
	}

	function handleClose() {
		closeScheduledDowngradeDialog();
	}
</script>

<AlertDialog.Root
	open={scheduledDowngradeDialog.open}
	onOpenChange={(open) => !open && handleClose()}
>
	<AlertDialog.Content
		class="border border-border shadow-none sm:max-w-[480px]"
		data-testid="downgrade-warning-dialog"
	>
		<AlertDialog.Header>
			<AlertDialog.Title>{m.billing_downgrade_scheduled_title()}</AlertDialog.Title>
			<AlertDialog.Description>
				{m.billing_downgrade_scheduled_desc({
					pending_plan: scheduledDowngradeDialog.pendingPlan,
					current_period_end: scheduledDowngradeDialog.currentPeriodEnd
						? formatDate(scheduledDowngradeDialog.currentPeriodEnd)
						: ''
				})}
			</AlertDialog.Description>
		</AlertDialog.Header>

		{#if scheduledDowngradeDialog.warnings.length > 0}
			<div class="space-y-2 py-3" data-testid="downgrade-warnings-list">
				<p class="text-xs font-semibold tracking-wider text-muted-foreground uppercase">
					{m.common_warning ? m.common_warning() : 'Peringatan'}
				</p>
				<ul class="space-y-2 text-xs text-muted-foreground">
					{#each scheduledDowngradeDialog.warnings as warning (warning.limit)}
						<li
							class="rounded border border-amber-200 bg-amber-50 p-2 text-amber-900 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-200"
							data-testid={`warning-item-${warning.limit}`}
						>
							{m.billing_downgrade_warning_item({
								limit: getLimitName(warning.limit),
								used: warning.used,
								max: warning.max
							})}
						</li>
					{/each}
				</ul>
			</div>
		{/if}

		<AlertDialog.Footer>
			<Button
				variant="default"
				class="min-h-[44px]"
				onclick={handleClose}
				data-testid="downgrade-acknowledge-btn"
			>
				{m.billing_downgrade_warning_acknowledge()}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
