<script lang="ts">
	import InvitationRow from './InvitationRow.svelte';
	import type { ComponentProps } from 'svelte';

	let props: ComponentProps<typeof InvitationRow> = $props();
</script>

<table>
	<tbody>
		<InvitationRow {...props} />
	</tbody>
</table>
