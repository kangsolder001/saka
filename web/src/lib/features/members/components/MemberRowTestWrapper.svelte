<script lang="ts">
	import MemberRow from './MemberRow.svelte';
	import type { ComponentProps } from 'svelte';

	let props: ComponentProps<typeof MemberRow> = $props();
</script>

<table>
	<tbody>
		<MemberRow {...props} />
	</tbody>
</table>
