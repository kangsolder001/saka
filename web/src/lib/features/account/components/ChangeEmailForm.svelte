<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { Loader2 } from 'lucide-svelte';
	import { createForm } from '$lib/forms/form.svelte';
	import { requestEmailChange } from '$lib/features/account/api';
	import { changeEmailSchema } from '$lib/features/account/schemas';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	const form = createForm({
		schema: changeEmailSchema,
		initial: {
			new_email: '',
			current_password: ''
		},
		onSubmit: async (values) => {
			await requestEmailChange(values.new_email, values.current_password);
			toast.success(m.account_change_email_success());
			form.reset();
		}
	});
</script>

<Card.Root class="border-border">
	<Card.Header>
		<Card.Title>{m.account_change_email_title()}</Card.Title>
		<Card.Description>{m.account_change_email_desc()}</Card.Description>
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
				<Label for="new_email">{m.account_new_email()}</Label>
				<Input
					id="new_email"
					name="new_email"
					type="email"
					autocomplete="email"
					required
					bind:value={form.values.new_email}
					class="h-10"
				/>
				{#if form.errors.new_email}
					<p class="text-xs text-destructive">{form.errors.new_email}</p>
				{/if}
			</div>

			<div class="space-y-2">
				<Label for="email_current_password">{m.account_current_password()}</Label>
				<Input
					id="email_current_password"
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
			<Button type="submit" disabled={form.submitting} class="min-h-[44px]">
				{#if form.submitting}
					<Loader2 class="mr-2 size-4 animate-spin" />
				{/if}
				{m.account_change_email_submit()}
			</Button>
		</Card.Footer>
	</form>
</Card.Root>
