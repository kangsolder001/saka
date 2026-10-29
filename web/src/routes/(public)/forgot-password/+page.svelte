<script lang="ts">
	import { Loader2, MailCheck } from 'lucide-svelte';
	import { createForm } from '$lib/forms/form.svelte';
	import { forgotPasswordSchema } from '$lib/features/auth/schemas';
	import { forgotPassword } from '$lib/features/auth/api';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as m from '$lib/paraglide/messages';

	let submitted = $state(false);

	const form = createForm({
		schema: forgotPasswordSchema,
		initial: { email: '' },
		onSubmit: async (values) => {
			try {
				await forgotPassword(values.email);
			} catch (err: unknown) {
				// Spec §6.4: always show the same message unless rate limited
				const hasStatus = typeof err === 'object' && err !== null && 'status' in err;
				if (hasStatus && (err as { status: number }).status === 429) {
					throw err;
				}
			}
			submitted = true;
		}
	});
</script>

<AuthCard
	title={m.auth_forgot_password_title()}
	description={submitted ? undefined : m.auth_forgot_password_desc()}
>
	{#if submitted}
		<div class="flex flex-col items-center justify-center py-4 text-center">
			<div
				class="mb-4 flex size-14 items-center justify-center rounded-full bg-primary/10 text-primary"
			>
				<MailCheck class="size-7" />
			</div>
			<p class="text-sm text-muted-foreground">
				{m.auth_forgot_password_success()}
			</p>
		</div>
		<Button href="/login" class="h-11 w-full">
			{m.auth_forgot_password_back_to_login()}
		</Button>
	{:else}
		<form onsubmit={form.submit} class="space-y-4">
			{#if form.formError}
				<div
					class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
					role="alert"
				>
					{form.formError}
				</div>
			{/if}

			<div class="space-y-2">
				<Label for="email">{m.auth_email()}</Label>
				<Input
					id="email"
					name="email"
					type="email"
					autocomplete="email"
					placeholder="name@example.com"
					bind:value={form.values.email}
					class="h-11 {form.errors.email
						? 'border-destructive focus-visible:ring-destructive'
						: ''}"
					disabled={form.submitting}
				/>
				{#if form.errors.email}
					<p class="text-xs text-destructive" role="alert">{form.errors.email}</p>
				{/if}
			</div>

			<Button type="submit" class="h-11 w-full" disabled={form.submitting}>
				{#if form.submitting}
					<Loader2 class="mr-2 size-4 animate-spin" />
					{m.auth_forgot_password_submitting()}
				{:else}
					{m.auth_forgot_password_submit()}
				{/if}
			</Button>
		</form>
	{/if}

	{#snippet footer()}
		{#if !submitted}
			<a href="/login" class="text-xs font-medium text-primary hover:underline">
				{m.auth_forgot_password_back_to_login()}
			</a>
		{/if}
	{/snippet}
</AuthCard>
