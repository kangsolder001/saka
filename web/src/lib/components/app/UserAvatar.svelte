<script lang="ts">
	import * as Avatar from '$lib/components/ui/avatar/index.js';
	import { cn } from '$lib/utils';

	interface Props {
		name?: string | null;
		email?: string | null;
		avatarUrl?: string | null;
		size?: 'sm' | 'default' | 'lg' | 'xl';
		class?: string;
	}

	let {
		name = null,
		email = null,
		avatarUrl = null,
		size = 'default',
		class: className
	}: Props = $props();

	// Valid avatar URL must start with http:// or https://
	const validAvatarUrl = $derived.by(() => {
		if (!avatarUrl || typeof avatarUrl !== 'string') return null;
		const trimmed = avatarUrl.trim();
		if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
			return trimmed;
		}
		return null;
	});

	// Derive initials from name or email
	const initials = $derived.by(() => {
		const target = name?.trim() || email?.trim() || '';
		if (!target) return '?';

		const parts = target.split(/\s+/).filter(Boolean);
		if (parts.length >= 2 && parts[0] && parts[1]) {
			const first = parts[0][0] ?? '';
			const second = parts[1][0] ?? '';
			return `${first}${second}`.toUpperCase();
		}
		return target.slice(0, 2).toUpperCase();
	});

	// Derive a stable, muted pastel color based on name/email hash
	const stableColorClass = $derived.by(() => {
		const target = name?.trim() || email?.trim() || 'default';
		let hash = 0;
		for (let i = 0; i < target.length; i++) {
			hash = (hash << 5) - hash + target.charCodeAt(i);
			hash |= 0;
		}

		const colors = [
			'bg-primary/10 text-primary',
			'bg-blue-500/10 text-blue-600 dark:text-blue-400',
			'bg-emerald-500/10 text-emerald-600 dark:text-emerald-400',
			'bg-amber-500/10 text-amber-600 dark:text-amber-400',
			'bg-purple-500/10 text-purple-600 dark:text-purple-400',
			'bg-rose-500/10 text-rose-600 dark:text-rose-400',
			'bg-teal-500/10 text-teal-600 dark:text-teal-400',
			'bg-indigo-500/10 text-indigo-600 dark:text-indigo-400'
		];

		const index = Math.abs(hash) % colors.length;
		return colors[index];
	});

	const sizeClasses = {
		sm: 'size-7 text-xs',
		default: 'size-9 text-sm',
		lg: 'size-12 text-base',
		xl: 'size-16 text-lg'
	};
</script>

<Avatar.Root class={cn(sizeClasses[size] || sizeClasses.default, 'font-medium', className)}>
	{#if validAvatarUrl}
		<Avatar.Image src={validAvatarUrl} alt={name || email || 'Avatar'} />
	{/if}
	<Avatar.Fallback class={cn('font-semibold', stableColorClass)}>
		{initials}
	</Avatar.Fallback>
</Avatar.Root>
