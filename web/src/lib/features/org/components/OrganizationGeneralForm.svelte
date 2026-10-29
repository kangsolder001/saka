<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { Loader2 } from 'lucide-svelte';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import { createForm } from '$lib/forms/form.svelte';
	import { updateOrganization } from '$lib/features/org/api';
	import { updateOrgSchema } from '$lib/features/org/schemas';
	import type { MemberOrganization, Organization } from '$lib/api/types';
	import UserAvatar from '$lib/components/app/UserAvatar.svelte';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Label } from '$lib/components/ui/label';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		org: Organization | MemberOrganization;
		disabled?: boolean;
	}

	let { org, disabled = false }: Props = $props();

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	let initialName = $state('');
	let initialSlug = $state('');
	let initialLogoUrl = $state('');
	let isInitialized = $state(false);

	const form = createForm({
		schema: updateOrgSchema,
		initial: {
			name: '',
			slug: '',
			logo_url: ''
		},
		onSubmit: async (values) => {
			const patch: { name?: string; slug?: string; logo_url?: string | null } = {};
			if (values.name !== undefined && values.name !== initialName) {
				patch.name = values.name;
			}
			if ((values.slug ?? '') !== initialSlug) {
				patch.slug = values.slug ? values.slug : undefined;
			}
			if ((values.logo_url ?? '') !== initialLogoUrl) {
				patch.logo_url = values.logo_url ? values.logo_url : null;
			}

			// If nothing changed, do not call API (PATCH rejects empty body)
			if (Object.keys(patch).length === 0) {
				return;
			}

			await updateOrganization(org.id, patch);
			await qc.invalidateQueries({ queryKey: queryKeys.organization(org.id) });
			await qc.invalidateQueries({ queryKey: queryKeys.me });

			initialName = values.name ?? initialName;
			initialSlug = values.slug ?? '';
			initialLogoUrl = values.logo_url ?? '';

			toast.success(m.org_settings_success());
		}
	});

	$effect(() => {
		if (org && !isInitialized) {
			initialName = org.name ?? '';
			initialSlug = org.slug ?? '';
			initialLogoUrl = org.logo_url ?? '';
			form.values.name = initialName;
			form.values.slug = initialSlug;
			form.values.logo_url = initialLogoUrl;
			isInitialized = true;
		}
	});

	let isDirty = $derived(
		form.values.name !== initialName ||
			(form.values.slug ?? '') !== initialSlug ||
			(form.values.logo_url ?? '') !== initialLogoUrl
	);
</script>

<Card.Root class="border-border shadow-none">
	<Card.Header>
		<Card.Title>{m.org_settings_title()}</Card.Title>
		<Card.Description>{m.org_settings_desc()}</Card.Description>
	</Card.Header>

	<form onsubmit={form.submit}>
		<Card.Content class="space-y-6">
			{#if form.formError}
				<div
					class="rounded-md border border-destructive/20 bg-danger-muted p-3 text-sm text-destructive"
					role="alert"
				>
					{form.formError}
				</div>
			{/if}

			<!-- Logo Preview & URL -->
			<div class="flex flex-col gap-4 sm:flex-row sm:items-center">
				<div class="shrink-0">
					<UserAvatar
						name={form.values.name || org.name}
						avatarUrl={form.values.logo_url}
						size="xl"
					/>
				</div>
				<div class="flex-1 space-y-2">
					<Label for="org-logo-url">{m.org_field_logo_url()}</Label>
					<Input
						id="org-logo-url"
						name="logo_url"
						type="url"
						placeholder={m.org_field_logo_url_placeholder()}
						disabled={disabled || form.submitting}
						bind:value={form.values.logo_url}
						class="h-10 min-h-[44px]"
					/>
					<p class="text-xs text-muted-foreground">{m.org_field_logo_url_help()}</p>
					{#if form.errors.logo_url}
						<p class="text-xs text-destructive">{form.errors.logo_url}</p>
					{/if}
				</div>
			</div>

			<!-- Organization Name -->
			<div class="space-y-2">
				<Label for="org-name">{m.org_field_name()}</Label>
				<Input
					id="org-name"
					name="name"
					type="text"
					required
					placeholder={m.org_field_name_placeholder()}
					disabled={disabled || form.submitting}
					bind:value={form.values.name}
					class="h-10 min-h-[44px]"
				/>
				{#if form.errors.name}
					<p class="text-xs text-destructive">{form.errors.name}</p>
				{/if}
			</div>

			<!-- Organization Slug -->
			<div class="space-y-2">
				<Label for="org-slug">{m.org_field_slug()}</Label>
				<Input
					id="org-slug"
					name="slug"
					type="text"
					placeholder={m.org_field_slug_placeholder()}
					disabled={disabled || form.submitting}
					bind:value={form.values.slug}
					class="h-10 min-h-[44px]"
				/>
				<p class="text-xs text-muted-foreground">{m.org_field_slug_help()}</p>
				{#if form.errors.slug}
					<p class="text-xs text-destructive">{form.errors.slug}</p>
				{/if}
			</div>
		</Card.Content>

		{#if !disabled}
			<Card.Footer class="flex justify-end border-t border-border pt-4">
				<Button
					type="submit"
					disabled={form.submitting || !isDirty}
					class="min-h-[44px] min-w-[120px]"
				>
					{#if form.submitting}
						<Loader2 class="mr-2 size-4 animate-spin" />
						{m.org_settings_saving()}
					{:else}
						{m.org_settings_save()}
					{/if}
				</Button>
			</Card.Footer>
		{/if}
	</form>
</Card.Root>
