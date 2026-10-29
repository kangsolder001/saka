import { page } from 'vitest/browser';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';
import AuthCard from './AuthCard.svelte';

describe('AuthCard', () => {
	it('renders title and description strings', async () => {
		render(AuthCard, {
			title: 'Masuk ke Akun',
			description: 'Masukkan kredensial Anda untuk melanjutkan'
		});

		await expect.element(page.getByText('Masuk ke Akun')).toBeInTheDocument();
		await expect
			.element(page.getByText('Masukkan kredensial Anda untuk melanjutkan'))
			.toBeInTheDocument();
	});
});
