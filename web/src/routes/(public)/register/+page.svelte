<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { Building2, Loader2 } from 'lucide-svelte';
	import { session } from '$lib/auth/session.svelte';
	import { safeRedirect } from '$lib/auth/guards';
	import { ApiError } from '$lib/api/errors';
	import { createForm } from '$lib/forms/form.svelte';
	import { registerSchema } from '$lib/features/auth/schemas';
	import { register } from '$lib/features/auth/api';
	import { getInvitation } from '$lib/features/invitations/api';
	import AuthCard from '$lib/components/app/AuthCard.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as m from '$lib/paraglide/messages';

	$effect(() => {
		if (session.status === 'authenticated') {
			const redirectUrl = safeRedirect(page.url.searchParams.get('redirect'), '/');
			void goto(redirectUrl);
		}
	});

	const invitationToken = $derived(page.url.searchParams.get('invitation'));
	let invitationOrgName = $state<string | null>(null);
	let loadingInvitation = $state<boolean>(false);

	const form = createForm({
		schema: registerSchema,
		initial: {
			name: '',
			email: '',
			password: '',
			organization_name: ''
		},
		onSubmit: async (values) => {
			if (invitationToken && !values.organization_name) {
				values.organization_name = values.name;
			}
			try {
				await register(values);
				const emailParam = encodeURIComponent(values.email);
				let targetUrl = `/register/check-email?email=${emailParam}`;
				if (invitationToken) {
					targetUrl += `&invitation=${encodeURIComponent(invitationToken)}`;
					if (invitationOrgName) {
						targetUrl += `&org=${encodeURIComponent(invitationOrgName)}`;
					}
				}
				await goto(targetUrl);
			} catch (err) {
				if (err instanceof ApiError && err.status === 409) {
					form.errors.email = m.auth_register_email_conflict();
					return;
				}
				throw err;
			}
		}
	});

	onMount(async () => {
		if (invitationToken) {
			loadingInvitation = true;
			try {
				const info = await getInvitation(invitationToken);
				form.values.email = info.email;
				invitationOrgName = info.organization.name;
				// Hidden field pre-fill with dummy value to satisfy client schema validation
				form.values.organization_name = info.organization.name;
			} catch {
				// Let the user fill manually if token fetch fails
			} finally {
				loadingInvitation = false;
			}
		}
	});
</script>

<AuthCard title={m.auth_register_title()} description={m.auth_register_desc()}>
	{#if invitationToken && invitationOrgName}
		<div
			class="mb-4 flex items-center gap-3 rounded-lg border border-primary/20 bg-primary/5 p-3 text-sm text-foreground"
			role="status"
		>
			<Building2 class="size-4 shrink-0 text-primary" />
			<span>{m.auth_register_invited_to({ organization: invitationOrgName })}</span>
		</div>
	{/if}

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
			<Label for="name">{m.auth_name()}</Label>
			<Input
				id="name"
				name="name"
				type="text"
				autocomplete="name"
				placeholder="Jane Doe"
				bind:value={form.values.name}
				class="h-11 {form.errors.name ? 'border-destructive focus-visible:ring-destructive' : ''}"
				disabled={form.submitting || loadingInvitation}
			/>
			{#if form.errors.name}
				<p class="text-xs text-destructive">{form.errors.name}</p>
			{/if}
		</div>

		<div class="space-y-2">
			<Label for="email">{m.auth_email()}</Label>
			<Input
				id="email"
				name="email"
				type="email"
				autocomplete="email"
				placeholder="name@example.com"
				bind:value={form.values.email}
				class="h-11 {form.errors.email ? 'border-destructive focus-visible:ring-destructive' : ''}"
				disabled={form.submitting || loadingInvitation || !!invitationToken}
				readonly={!!invitationToken}
			/>
			{#if form.errors.email}
				<p class="text-xs text-destructive">{form.errors.email}</p>
			{/if}
		</div>

		<div class="space-y-2">
			<Label for="password">{m.auth_password()}</Label>
			<Input
				id="password"
				name="password"
				type="password"
				autocomplete="new-password"
				bind:value={form.values.password}
				class="h-11 {form.errors.password
					? 'border-destructive focus-visible:ring-destructive'
					: ''}"
				disabled={form.submitting || loadingInvitation}
			/>
			{#if form.errors.password}
				<p class="text-xs text-destructive">{form.errors.password}</p>
			{/if}
		</div>

		{#if !invitationToken}
			<div class="space-y-2">
				<Label for="organization_name">{m.auth_organization_name()}</Label>
				<Input
					id="organization_name"
					name="organization_name"
					type="text"
					placeholder="Acme Corp"
					bind:value={form.values.organization_name}
					class="h-11 {form.errors.organization_name
						? 'border-destructive focus-visible:ring-destructive'
						: ''}"
					disabled={form.submitting || loadingInvitation}
				/>
				{#if form.errors.organization_name}
					<p class="text-xs text-destructive">{form.errors.organization_name}</p>
				{/if}
			</div>
		{/if}

		<Button type="submit" class="h-11 w-full" disabled={form.submitting || loadingInvitation}>
			{#if form.submitting || loadingInvitation}
				<Loader2 class="mr-2 size-4 animate-spin" />
				{m.auth_register_submitting()}
			{:else}
				{m.auth_register_submit()}
			{/if}
		</Button>
	</form>

	{#snippet footer()}
		<p class="text-xs text-muted-foreground">
			{m.auth_register_has_account()}
			<a href="/login" class="font-medium text-primary hover:underline">
				{m.auth_register_login_link()}
			</a>
		</p>
	{/snippet}
</AuthCard>
