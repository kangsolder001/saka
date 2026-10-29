import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import EmptyState from './EmptyState.svelte';

describe('EmptyState', () => {
	it('renders title and description', async () => {
		render(EmptyState, {
			title: 'Belum Ada Anggota',
			description: 'Undang rekan kerja untuk mulai berkolaborasi'
		});

		await expect.element(page.getByText('Belum Ada Anggota')).toBeInTheDocument();
		await expect
			.element(page.getByText('Undang rekan kerja untuk mulai berkolaborasi'))
			.toBeInTheDocument();
	});
});
