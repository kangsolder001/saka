<script lang="ts">
	import { Skeleton } from '$lib/components/ui/skeleton';
</script>

<div class="flex w-full flex-col gap-6 py-4" data-testid="page-skeleton">
	<div class="flex items-center justify-between">
		<div class="flex flex-col gap-2">
			<Skeleton class="h-8 w-48" />
			<Skeleton class="h-4 w-72" />
		</div>
		<Skeleton class="h-10 w-28" />
	</div>

	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
		<Skeleton class="h-32 w-full rounded-xl" />
		<Skeleton class="h-32 w-full rounded-xl" />
		<Skeleton class="h-32 w-full rounded-xl" />
	</div>

	<div class="flex flex-col gap-3">
		<Skeleton class="h-12 w-full rounded-lg" />
		<Skeleton class="h-12 w-full rounded-lg" />
		<Skeleton class="h-12 w-full rounded-lg" />
	</div>
</div>
