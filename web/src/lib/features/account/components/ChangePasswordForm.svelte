<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { Loader2 } from 'lucide-svelte';
	import { createForm } from '$lib/forms/form.svelte';
	import { changePassword } from '$lib/features/account/api';
	import { changePasswordSchema } from '$lib/features/account/schemas';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	const form = createForm({
		schema: changePasswordSchema,
		initial: {
			current_password: '',
			new_password: '',
			confirm_password: ''
		},
		onSubmit: async (values) => {
			await changePassword(values.current_password, values.new_password);
			toast.success(m.account_change_password_success());
			form.reset();
		}
	});
</script>

<Card.Root class="border-border">
	<Card.Header>
		<Card.Title>{m.account_change_password_title()}</Card.Title>
		<Card.Description>{m.account_change_password_desc()}</Card.Description>
	</Card.Header>

	<form onsubmit={form.submit}>
		<Card.Content class="space-y-4">
			{#if form.formError}
				<div
					class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
					role="alert"
				>
					{form.formError}
				</div>
			{/if}

			<div class="space-y-2">
				<Label for="current_password">{m.account_current_password()}</Label>
				<Input
					id="current_password"
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

			<div class="space-y-2">
				<Label for="new_password">{m.account_new_password()}</Label>
				<Input
					id="new_password"
					name="new_password"
					type="password"
					autocomplete="new-password"
					required
					bind:value={form.values.new_password}
					class="h-10"
				/>
				{#if form.errors.new_password}
					<p class="text-xs text-destructive">{form.errors.new_password}</p>
				{/if}
			</div>

			<div class="space-y-2">
				<Label for="confirm_password">{m.account_confirm_new_password()}</Label>
				<Input
					id="confirm_password"
					name="confirm_password"
					type="password"
					autocomplete="new-password"
					required
					bind:value={form.values.confirm_password}
					class="h-10"
				/>
				{#if form.errors.confirm_password}
					<p class="text-xs text-destructive">{form.errors.confirm_password}</p>
				{/if}
			</div>
		</Card.Content>

		<Card.Footer class="flex justify-end border-t border-border pt-4">
			<Button type="submit" disabled={form.submitting} class="min-h-[44px]">
				{#if form.submitting}
					<Loader2 class="mr-2 size-4 animate-spin" />
				{/if}
				{m.account_change_password_submit()}
			</Button>
		</Card.Footer>
	</form>
</Card.Root>
