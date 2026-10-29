<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import { errorMessage } from '$lib/api/error-message';
	import { clearLastOrgId, leaveOrganization } from '$lib/features/org/api';
	import { can } from '$lib/features/org/permissions';
	import type { Role } from '$lib/api/types';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		orgId: string;
		role: Role;
		onLeave: () => void;
	}

	let { orgId, role, onLeave }: Props = $props();

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	let showConfirm = $state(false);
	let isLeaving = $state(false);

	async function handleLeave() {
		isLeaving = true;
		try {
			await leaveOrganization(orgId);
			clearLastOrgId();
			await qc.invalidateQueries({ queryKey: queryKeys.me });
			toast.success(m.org_leave_success());
			onLeave();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			isLeaving = false;
		}
	}
</script>

{#if can(role, 'org.leave')}
	<Card.Root class="border-border shadow-none">
		<Card.Header>
			<Card.Title>{m.org_leave_title()}</Card.Title>
			<Card.Description>{m.org_leave_desc()}</Card.Description>
		</Card.Header>

		<Card.Footer class="flex justify-end border-t border-border pt-4">
			<Button variant="destructive" class="min-h-[44px]" onclick={() => (showConfirm = true)}>
				{m.org_leave_submit()}
			</Button>
		</Card.Footer>
	</Card.Root>

	<ConfirmDialog
		bind:open={showConfirm}
		title={m.org_leave_confirm_title()}
		description={m.org_leave_confirm_desc()}
		confirmLabel={m.org_leave_submit()}
		cancelLabel={m.common_cancel()}
		variant="destructive"
		loading={isLeaving}
		onConfirm={handleLeave}
	/>
{/if}
