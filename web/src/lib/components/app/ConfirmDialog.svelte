<script lang="ts">
	import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import * as m from '$lib/paraglide/messages.js';

	interface Props {
		open?: boolean;
		title: string;
		description: string;
		confirmLabel?: string;
		cancelLabel?: string;
		variant?: 'default' | 'destructive';
		confirmText?: string;
		loading?: boolean;
		onConfirm: () => void | Promise<void>;
		onCancel?: () => void;
	}

	let {
		open = $bindable(false),
		title,
		description,
		confirmLabel = m.dialog_confirm(),
		cancelLabel = m.common_cancel(),
		variant = 'default',
		confirmText,
		loading = false,
		onConfirm,
		onCancel
	}: Props = $props();

	let typedText = $state('');

	let canConfirm = $derived(!loading && (!confirmText || typedText.trim() === confirmText.trim()));

	function handleOpenChange(nextOpen: boolean) {
		open = nextOpen;
		if (!nextOpen) {
			typedText = '';
			onCancel?.();
		}
	}

	async function handleConfirm() {
		if (!canConfirm) return;
		await onConfirm();
		open = false;
		typedText = '';
	}

	function handleCancel() {
		open = false;
		typedText = '';
		onCancel?.();
	}
</script>

<AlertDialog.Root {open} onOpenChange={handleOpenChange}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>{title}</AlertDialog.Title>
			<AlertDialog.Description>{description}</AlertDialog.Description>
		</AlertDialog.Header>

		{#if confirmText}
			<div class="my-2 space-y-2">
				<p class="text-xs text-muted-foreground">
					{m.dialog_type_confirm_label({ text: confirmText })}
				</p>
				<Input
					type="text"
					bind:value={typedText}
					placeholder={confirmText}
					disabled={loading}
					class="w-full"
				/>
			</div>
		{/if}

		<AlertDialog.Footer>
			<AlertDialog.Cancel onclick={handleCancel} disabled={loading} autofocus>
				{cancelLabel}
			</AlertDialog.Cancel>
			<Button
				variant={variant === 'destructive' ? 'destructive' : 'default'}
				disabled={!canConfirm}
				onclick={handleConfirm}
			>
				{confirmLabel}
			</Button>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
