<script lang="ts">
	import { onMount, type Snippet } from 'svelte';
	import { AlertCircle, CheckCircle2, Loader2 } from 'lucide-svelte';
	import { ApiError } from '$lib/api/errors';
	import { errorMessage } from '$lib/api/error-message';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		title: string;
		loadingText: string;
		successTitle: string;
		successMessage: string;
		primaryButtonLabel: string;
		primaryButtonHref: string;
		action: () => Promise<void>;
		onSuccess?: () => void;
		errorOverride?: (err: unknown) => string | null;
		children?: Snippet;
	}

	let {
		title,
		loadingText,
		successTitle,
		successMessage,
		primaryButtonLabel,
		primaryButtonHref,
		action,
		onSuccess,
		errorOverride,
		children
	}: Props = $props();

	type ActionState = 'loading' | 'success' | 'error';
	let actionState: ActionState = $state('loading');
	let errorText: string | null = $state(null);

	let hasExecuted = false;

	onMount(async () => {
		if (hasExecuted) return;
		hasExecuted = true;

		try {
			await action();
			actionState = 'success';
			onSuccess?.();
		} catch (err: unknown) {
			actionState = 'error';
			const customMsg = errorOverride?.(err);
			if (customMsg) {
				errorText = customMsg;
			} else if (err instanceof ApiError) {
				if (err.status === 400 && err.fieldErrors.token) {
					errorText = m.field_error_token_invalid();
				} else {
					errorText = errorMessage(err);
				}
			} else {
				errorText = errorMessage(err);
			}
		}
	});
</script>

<AuthCard title={actionState === 'success' ? successTitle : title}>
	{#if actionState === 'loading'}
		<div class="flex flex-col items-center justify-center py-6 text-center">
			<Loader2 class="mb-4 size-8 animate-spin text-primary" />
			<p class="text-sm text-muted-foreground">{loadingText}</p>
		</div>
	{:else if actionState === 'success'}
		<div class="flex flex-col items-center justify-center py-6 text-center">
			<div
				class="mb-4 flex size-14 items-center justify-center rounded-full bg-success-muted text-success"
			>
				<CheckCircle2 class="size-7" />
			</div>
			<p class="text-sm text-muted-foreground">{successMessage}</p>
		</div>

		{#if children}
			{@render children()}
		{/if}

		<Button href={primaryButtonHref} class="h-11 w-full">
			{primaryButtonLabel}
		</Button>
	{:else}
		<div class="flex flex-col items-center justify-center py-6 text-center">
			<div
				class="mb-4 flex size-14 items-center justify-center rounded-full bg-danger-muted text-destructive"
			>
				<AlertCircle class="size-7" />
			</div>
			<p class="text-sm text-destructive" role="alert">
				{errorText || m.field_error_token_invalid()}
			</p>
		</div>

		<Button href="/login" variant="outline" class="h-11 w-full">
			{m.auth_login_submit()}
		</Button>
	{/if}
</AuthCard>
