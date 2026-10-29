<script lang="ts">
	import * as m from '$lib/paraglide/messages.js';
	import type { Role } from '$lib/api/types.js';
	import { cn } from '$lib/utils.js';

	interface Props {
		role: Role | string;
		class?: string;
	}

	let { role, class: className }: Props = $props();

	let label = $derived.by<string>(() => {
		switch (role) {
			case 'owner':
				return m.badge_role_owner();
			case 'admin':
				return m.badge_role_admin();
			case 'member':
				return m.badge_role_member();
			default:
				return role;
		}
	});
</script>

<span
	data-slot="role-badge"
	data-role={role}
	class={cn(
		'inline-flex items-center rounded-full border border-border bg-muted/60 px-2 py-0.5 text-xs font-medium text-foreground',
		className
	)}
>
	{label}
</span>
