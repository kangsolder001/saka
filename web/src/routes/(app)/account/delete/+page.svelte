<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { AlertTriangle, Loader2 } from 'lucide-svelte';
	import { ApiError } from '$lib/api/errors';
	import { logout } from '$lib/auth/logout';
	import { createForm } from '$lib/forms/form.svelte';
	import { deleteMe } from '$lib/features/account/api';
	import { deleteAccountSchema } from '$lib/features/account/schemas';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	let confirmDialogOpen = $state(false);
	let isConflictOwnsShared = $state(false);

	const form = createForm({
		schema: deleteAccountSchema,
		initial: {
			current_password: ''
		},
		onSubmit: async (values) => {
			isConflictOwnsShared = false;
			try {
				await deleteMe(values.current_password);
				toast.success(m.account_delete_success());
				await logout();
			} catch (err: unknown) {
				if (err instanceof ApiError && err.status === 409) {
					if (err.reason === 'owns_shared_organization') {
						isConflictOwnsShared = true;
						return;
					}
				}
				throw err;
			}
		}
	});

	function handleSubmitRequest(e: SubmitEvent) {
		e.preventDefault();
		// Validate first before opening confirm dialog
		const parsed = deleteAccountSchema.safeParse(form.values);
		if (!parsed.success) {
			form.submit(e);
			return;
		}
		confirmDialogOpen = true;
	}

	function handleConfirmedDelete() {
		confirmDialogOpen = false;
		void form.submit();
	}
</script>

<Card.Root class="border-destructive/30">
	<Card.Header>
		<Card.Title class="text-destructive">{m.account_delete_title()}</Card.Title>
		<Card.Description>{m.account_delete_desc()}</Card.Description>
	</Card.Header>

	<form onsubmit={handleSubmitRequest}>
		<Card.Content class="space-y-6">
			<!-- Warning Banner -->
			<div
				class="flex items-start gap-3 rounded-lg border border-amber-500/20 bg-amber-500/10 p-4 text-sm text-foreground"
			>
				<AlertTriangle class="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
				<div class="space-y-1">
					<p class="font-medium text-amber-800 dark:text-amber-300">
						{m.common_warning()}
					</p>
					<p class="text-muted-foreground">{m.account_delete_warning()}</p>
				</div>
			</div>

			{#if isConflictOwnsShared}
				<div
					class="space-y-2 rounded-lg border border-destructive/20 bg-danger-muted p-4 text-sm text-destructive"
					role="alert"
				>
					<p class="font-medium">{m.account_delete_owns_shared()}</p>
					<a
						href="/orgs"
						class="inline-block font-semibold underline underline-offset-4 hover:text-destructive/80"
					>
						{m.account_delete_transfer_link()} →
					</a>
				</div>
			{/if}

			{#if form.formError && !isConflictOwnsShared}
				<div
					class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
					role="alert"
				>
					{form.formError}
				</div>
			{/if}

			<div class="space-y-2">
				<Label for="delete_current_password">{m.account_delete_password_confirm()}</Label>
				<Input
					id="delete_current_password"
					name="current_password"
					type="password"
					autocomplete="current-password"
					required
					bind:value={form.values.current_password}
					class="h-10"
				/>
				{#if form.errors.current_password}
					<p class="text-xs text-destructive">{form.errors.current_password}</p>
				{/if}
			</div>
		</Card.Content>

		<Card.Footer class="flex justify-end border-t border-border pt-4">
			<Button type="submit" variant="destructive" disabled={form.submitting} class="min-h-[44px]">
				{#if form.submitting}
					<Loader2 class="mr-2 size-4 animate-spin" />
				{/if}
				{m.account_delete_btn()}
			</Button>
		</Card.Footer>
	</form>
</Card.Root>

<ConfirmDialog
	bind:open={confirmDialogOpen}
	title={m.account_delete_confirm_title()}
	description={m.account_delete_confirm_desc()}
	confirmLabel={m.account_delete_btn()}
	variant="destructive"
	loading={form.submitting}
	onConfirm={handleConfirmedDelete}
	onCancel={() => {
		confirmDialogOpen = false;
	}}
/>
