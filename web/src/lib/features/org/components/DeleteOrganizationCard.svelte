<script lang="ts">
	import { toast } from 'svelte-sonner';
	import { useQueryClient } from '@tanstack/svelte-query';
	import { queryClient as defaultQueryClient } from '$lib/api/query-client';
	import { queryKeys } from '$lib/api/query-keys';
	import { errorMessage } from '$lib/api/error-message';
	import { clearLastOrgId, deleteOrganization } from '$lib/features/org/api';
	import { can } from '$lib/features/org/permissions';
	import type { Role } from '$lib/api/types';
	import ConfirmDialog from '$lib/components/app/ConfirmDialog.svelte';
	import { Button } from '$lib/components/ui/button';
	import * as Card from '$lib/components/ui/card';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		orgId: string;
		orgName: string;
		role: Role;
		onDelete: () => void;
	}

	let { orgId, orgName, role, onDelete }: Props = $props();

	let qc: ReturnType<typeof useQueryClient>;
	try {
		qc = useQueryClient();
	} catch {
		qc = defaultQueryClient;
	}

	let showConfirm = $state(false);
	let isDeleting = $state(false);

	async function handleDelete() {
		isDeleting = true;
		try {
			await deleteOrganization(orgId);
			clearLastOrgId();
			await qc.invalidateQueries({ queryKey: queryKeys.me });
			toast.success(m.org_delete_success());
			onDelete();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			isDeleting = false;
		}
	}
</script>

{#if can(role, 'org.delete')}
	<Card.Root class="border-destructive/30 bg-destructive/5 shadow-none">
		<Card.Header>
			<Card.Title class="text-destructive">{m.org_delete_title()}</Card.Title>
			<Card.Description>{m.org_delete_desc()}</Card.Description>
		</Card.Header>

		<Card.Footer class="flex justify-end border-t border-destructive/20 pt-4">
			<Button variant="destructive" class="min-h-[44px]" onclick={() => (showConfirm = true)}>
				{m.org_delete_submit()}
			</Button>
		</Card.Footer>
	</Card.Root>

	<ConfirmDialog
		bind:open={showConfirm}
		title={m.org_delete_confirm_title()}
		description={m.org_delete_confirm_desc()}
		confirmText={orgName}
		confirmLabel={m.org_delete_submit()}
		cancelLabel={m.common_cancel()}
		variant="destructive"
		loading={isDeleting}
		onConfirm={handleDelete}
	/>
{/if}
