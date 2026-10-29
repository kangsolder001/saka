<script lang="ts">
	import { page } from '$app/state';
	import { createOrganizationQuery } from '$lib/features/org/api';
	import { createMembersQuery } from '$lib/features/members/api';
	import { createInvitationsQuery } from '$lib/features/invitations/api';
	import { createSubscriptionQuery } from '$lib/features/billing/api';
	import { upgradeDialog } from '$lib/features/billing/upgrade.svelte';
	import { session } from '$lib/auth/session.svelte';
	import { createMeQuery } from '$lib/features/account/api';
	import { can } from '$lib/features/org/permissions';
	import PageHeader from '$lib/components/app/PageHeader.svelte';
	import PageSkeleton from '$lib/components/app/PageSkeleton.svelte';
	import ErrorState from '$lib/components/app/ErrorState.svelte';
	import EmptyState from '$lib/components/app/EmptyState.svelte';
	import LoadMore from '$lib/components/app/LoadMore.svelte';
	import UpgradeDialog from '$lib/components/app/UpgradeDialog.svelte';
	import MemberUsage from '$lib/features/members/components/MemberUsage.svelte';
	import MemberRow from '$lib/features/members/components/MemberRow.svelte';
	import InvitationRow from '$lib/features/invitations/components/InvitationRow.svelte';
	import InviteDialog from '$lib/features/invitations/components/InviteDialog.svelte';
	import * as Tabs from '$lib/components/ui/tabs/index.js';
	import * as Table from '$lib/components/ui/table/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { UserPlus } from 'lucide-svelte';
	import * as m from '$lib/paraglide/messages.js';

	let orgId = $derived(page.params.orgId ?? '');

	const orgQuery = createOrganizationQuery(() => orgId);
	const meQuery = createMeQuery();

	let callerRole = $derived(orgQuery.data?.organization?.role ?? 'member');
	let selfUserId = $derived(session.user?.id ?? meQuery.data?.user?.id ?? '');
	let canManageInvitations = $derived(can(callerRole, 'invitations.manage'));

	const membersQuery = createMembersQuery(() => orgId);
	const invitationsQuery = createInvitationsQuery(() => (canManageInvitations ? orgId : ''));
	const subscriptionQuery = createSubscriptionQuery(() => orgId);

	let inviteDialogOpen = $state(false);
	let activeTab = $state('members');

	let allMembers = $derived(membersQuery.data?.pages.flatMap((p) => p.items ?? []) ?? []);
	let allInvitations = $derived(invitationsQuery.data?.pages.flatMap((p) => p.items ?? []) ?? []);

	let usedMembers = $derived(subscriptionQuery.data?.usage?.members?.used ?? allMembers.length);
	let maxMembers = $derived(subscriptionQuery.data?.usage?.members?.max);

	let hasMoreMembers = $derived(Boolean(membersQuery.hasNextPage));
	let isFetchingMoreMembers = $derived(Boolean(membersQuery.isFetchingNextPage));

	let hasMoreInvitations = $derived(Boolean(invitationsQuery.hasNextPage));
	let isFetchingMoreInvitations = $derived(Boolean(invitationsQuery.isFetchingNextPage));
</script>

<div class="space-y-6 p-6" data-testid="members-page">
	<PageHeader title={m.members_title()} description={m.members_desc()}>
		{#snippet actions()}
			{#if canManageInvitations}
				<Button
					onclick={() => (inviteDialogOpen = true)}
					class="min-h-[44px] gap-2"
					data-testid="invite-member-btn"
				>
					<UserPlus class="size-4" />
					<span>{m.members_invite_btn()}</span>
				</Button>
			{/if}
		{/snippet}
	</PageHeader>

	<Tabs.Root value={activeTab} onValueChange={(val) => (activeTab = val)} class="w-full space-y-4">
		<div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
			<Tabs.List class="h-10">
				<Tabs.Trigger value="members" class="min-h-[36px]" data-testid="tab-trigger-members">
					{m.members_tab_members()}
				</Tabs.Trigger>
				<Tabs.Trigger
					value="invitations"
					class="min-h-[36px]"
					data-testid="tab-trigger-invitations"
				>
					{m.members_tab_invitations()}
					{#if allInvitations.length > 0}
						<span
							class="ml-1.5 rounded-full bg-muted px-1.5 py-0.5 text-xs font-semibold text-muted-foreground"
						>
							{allInvitations.length}
						</span>
					{/if}
				</Tabs.Trigger>
			</Tabs.List>

			{#if activeTab === 'members'}
				<MemberUsage used={usedMembers} max={maxMembers} />
			{/if}
		</div>

		<Tabs.Content value="members" class="space-y-4" data-testid="tab-content-members">
			{#if membersQuery.isPending}
				<PageSkeleton />
			{:else if membersQuery.isError}
				<ErrorState error={membersQuery.error} retry={() => membersQuery.refetch()} />
			{:else}
				<div class="rounded-lg border border-border bg-card">
					<Table.Root>
						<Table.Header>
							<Table.Row class="hover:bg-transparent">
								<Table.Head>{m.members_tab_members()}</Table.Head>
								<Table.Head>{m.invite_field_role()}</Table.Head>
								<Table.Head>{m.members_joined()}</Table.Head>
								<Table.Head class="text-right"><span class="sr-only">Actions</span></Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each allMembers as member (member.user_id)}
								<MemberRow
									{member}
									{callerRole}
									{selfUserId}
									{orgId}
									onChanged={() => {
										membersQuery.refetch();
										subscriptionQuery.refetch();
									}}
								/>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>

				<LoadMore
					hasMore={hasMoreMembers}
					loading={isFetchingMoreMembers}
					onLoadMore={() => membersQuery.fetchNextPage()}
				/>
			{/if}
		</Tabs.Content>

		<Tabs.Content value="invitations" class="space-y-4" data-testid="tab-content-invitations">
			{#if invitationsQuery.isPending}
				<PageSkeleton />
			{:else if invitationsQuery.isError}
				<ErrorState error={invitationsQuery.error} retry={() => invitationsQuery.refetch()} />
			{:else if allInvitations.length === 0}
				<EmptyState title={m.invitations_empty_title()} description={m.invitations_empty_desc()} />
			{:else}
				<div class="rounded-lg border border-border bg-card">
					<Table.Root>
						<Table.Header>
							<Table.Row class="hover:bg-transparent">
								<Table.Head>{m.invitations_col_email()}</Table.Head>
								<Table.Head>{m.invitations_col_role()}</Table.Head>
								<Table.Head>{m.invitations_col_expires()}</Table.Head>
								<Table.Head class="text-right"><span class="sr-only">Actions</span></Table.Head>
							</Table.Row>
						</Table.Header>
						<Table.Body>
							{#each allInvitations as invitation (invitation.id)}
								<InvitationRow
									{invitation}
									{orgId}
									onChanged={() => {
										invitationsQuery.refetch();
										subscriptionQuery.refetch();
									}}
								/>
							{/each}
						</Table.Body>
					</Table.Root>
				</div>

				<LoadMore
					hasMore={hasMoreInvitations}
					loading={isFetchingMoreInvitations}
					onLoadMore={() => invitationsQuery.fetchNextPage()}
				/>
			{/if}
		</Tabs.Content>
	</Tabs.Root>

	<InviteDialog
		bind:open={inviteDialogOpen}
		{orgId}
		onSuccess={() => {
			membersQuery.refetch();
			invitationsQuery.refetch();
			subscriptionQuery.refetch();
		}}
	/>

	<UpgradeDialog bind:open={upgradeDialog.open} orgId={upgradeDialog.orgId || orgId} />
</div>
