<script lang="ts">
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { createOrganizationQuery } from '$lib/features/org/api';
	import { createMeQuery } from '$lib/features/account/api';
	import { can } from '$lib/features/org/permissions';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import OrganizationGeneralForm from '$lib/features/org/components/OrganizationGeneralForm.svelte';
	import TransferOwnershipCard from '$lib/features/org/components/TransferOwnershipCard.svelte';
	import LeaveOrganizationCard from '$lib/features/org/components/LeaveOrganizationCard.svelte';
	import DeleteOrganizationCard from '$lib/features/org/components/DeleteOrganizationCard.svelte';
	import * as m from '$lib/paraglide/messages';

	let orgId = $derived(page.params.orgId ?? '');
	const orgQuery = createOrganizationQuery(() => orgId);
	const meQuery = createMeQuery();

	let org = $derived(orgQuery.data?.organization);
	let selfUserId = $derived(meQuery.data?.user?.id ?? '');
</script>

{#if (orgQuery.isPending || meQuery.isPending) && !org}
	<PageSkeleton />
{:else if orgQuery.isError}
	<ErrorState error={orgQuery.error} retry={() => orgQuery.refetch()} />
{:else if org}
	<div class="space-y-6">
		<PageHeader title={m.org_settings_title()} description={m.org_settings_desc()} />

		<OrganizationGeneralForm {org} disabled={!can(org.role, 'org.update')} />

		{#if can(org.role, 'ownership.transfer')}
			<TransferOwnershipCard orgId={org.id} role={org.role} {selfUserId} />
		{/if}

		{#if can(org.role, 'org.leave')}
			<LeaveOrganizationCard orgId={org.id} role={org.role} onLeave={() => goto('/')} />
		{/if}

		{#if can(org.role, 'org.delete')}
			<DeleteOrganizationCard
				orgId={org.id}
				orgName={org.name}
				role={org.role}
				onDelete={() => goto('/')}
			/>
		{/if}
	</div>
{/if}
