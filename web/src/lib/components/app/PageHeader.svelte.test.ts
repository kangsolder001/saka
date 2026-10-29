import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import PageHeader from './PageHeader.svelte';

describe('PageHeader', () => {
	it('renders title and description', async () => {
		render(PageHeader, {
			title: 'Pengaturan Organisasi',
			description: 'Kelola informasi dan preferensi organisasi'
		});

		await expect.element(page.getByText('Pengaturan Organisasi')).toBeInTheDocument();
		await expect
			.element(page.getByText('Kelola informasi dan preferensi organisasi'))
			.toBeInTheDocument();
	});
});
