<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { buttonVariants } from '$lib/components/ui/button/index.js';
	import * as m from '$lib/paraglide/messages.js';

	interface Props {
		open?: boolean;
		orgId: string;
	}

	let { open = $bindable(false), orgId }: Props = $props();

	function handleOpenChange(nextOpen: boolean) {
		open = nextOpen;
	}

	function handleCancel() {
		open = false;
	}
</script>

<AlertDialog.Root {open} onOpenChange={handleOpenChange}>
	<AlertDialog.Content data-testid="upgrade-dialog">
		<AlertDialog.Header>
			<AlertDialog.Title>{m.upgrade_dialog_title()}</AlertDialog.Title>
			<AlertDialog.Description>{m.upgrade_dialog_desc()}</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel class="min-h-[44px]" onclick={handleCancel}>
				{m.upgrade_dialog_cancel()}
			</AlertDialog.Cancel>
			<a
				href="/orgs/{orgId}/billing/plans"
				class={buttonVariants({ variant: 'default', class: 'min-h-[44px]' })}
				onclick={() => {
					open = false;
				}}
				data-testid="upgrade-dialog-btn"
			>
				{m.upgrade_dialog_btn()}
			</a>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
