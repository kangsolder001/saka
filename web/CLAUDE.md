# CLAUDE.md — Frontend Web

Panduan untuk agent yang bekerja di `web/`. Berlaku bersama `CLAUDE.md` root (aturan Git, bahasa dokumen, dan keputusan backend tetap berlaku).

## Status

Belum ada kode; fase berikutnya **W0**. Acuan progres adalah centang di `web/docs/todo.md`, bukan ringkasan ini.

## Dokumen (baca dalam urutan ini)

1. `web/docs/spec.md`: sumber kebenaran frontend (halaman, alur auth, error, billing, tema, i18n).
2. `web/docs/plan.md`: urutan fase dan keputusan yang sudah final.
3. `web/docs/todo.md`: checklist per tugas. **Kerjakan satu tugas per sesi**, centang setelah selesai.
4. `docs/openapi.yaml` (root): kontrak API. Perilaku rinci backend di `docs/spec.md` (root).

Dokumen ditulis dalam Bahasa Indonesia; ikuti bahasa yang sama saat mengubahnya.

## Keputusan final (jangan diulang tanyakan)

SvelteKit + Svelte 5 (runes) mode SPA statis, TypeScript strict, pnpm, Tailwind v4 + shadcn-svelte, TanStack Query, openapi-typescript + openapi-fetch, Zod + helper form sendiri (bukan superforms), Paraglide (id dasar + en, tanpa prefix URL), mode-watcher (light/dark/system), Vitest + Playwright. Tema putih & biru (light) serta hitam & biru dark (dark), menghindari tampilan klise AI generator (token di spec §4.1). Icon vektor SVG (lucide-svelte / flaticon / SVG); dilarang keras memakai emotikon/emoji di UI.

## Perintah

Dari root repo: `make web-install`, `make web-dev` (port 3000, proxy `/api` ke `:8080`), `make web-api` (generate tipe dari `docs/openapi.yaml`), `make web-check` (lint + svelte-check + unit test + build), `make web-e2e`. Backend untuk dev: `make up migrate run` dan `make run-worker` (email ke Mailpit `http://localhost:8025`); `make seed` membuat akun demo.

**Sebelum menyatakan tugas selesai, `make web-check` harus hijau.**

## Aturan kode (wajib)

- **Bahasa Inggris untuk semua kode:** nama variabel, fungsi, tipe, file, komponen, kunci i18n, komentar. Teks UI ditulis lewat i18n dalam Bahasa Indonesia dan Inggris.
- **Svelte 5 runes saja:** `$state`, `$derived`, `$props`, `$effect`, snippet. Jangan pakai sintaks lama (`export let`, `$:`, `<slot>`, `createEventDispatcher`, `on:click`; pakai `onclick`).
- **Jangan menebak API library.** Versi yang dipakai baru (SvelteKit 3, Svelte 5, Tailwind 4, Zod 4, Paraglide 2, TanStack Query 6). Bila ragu, baca dokumentasi resmi dulu (mis. `https://svelte.dev/llms-small.txt`, dokumentasi shadcn-svelte, TanStack Query Svelte). Ikuti contoh yang sudah ada di repo.
- **Tidak ada `fetch` di komponen.** Semua panggilan API lewat `src/lib/api/client.ts` dan fungsi di `src/lib/features/<fitur>/api.ts`. Query key hanya dari `src/lib/api/query-keys.ts`.
- **Tidak ada tipe API tulisan tangan.** Pakai alias di `src/lib/api/types.ts` (dari `schema.d.ts` hasil generate). Jangan edit `schema.d.ts`; jalankan `make web-api`.
- **Tidak ada teks hardcode.** Tambahkan kunci ke `messages/id.json` **dan** `messages/en.json` (kunci `snake_case` berawalan area, mis. `members_invite_button`).
- **Dilarang memakai emotikon/emoji di UI:** Jangan gunakan emoji (mis. 🚀, 💡, 🌙, ☀️, ✕, dsb.) pada tombol, status badge, empty state, header, atau ilustrasi. Seluruh icon visual wajib berupa icon vektor SVG profesional (mis. `lucide-svelte` bawaan shadcn-svelte, Flaticon SVG, atau Tabler SVG) dengan ukuran dan stroke konsisten.
- **Tidak ada warna mentah.** Hanya kelas token (`bg-primary`, `text-muted-foreground`, `text-success`, `bg-warning-muted`, ...). Dilarang `bg-blue-600`, `text-red-500`, hex di komponen. Gunakan skema token putih-biru (light) dan hitam-biru dark (dark).
- **Desain responsif wajib (mobile & desktop):** Seluruh halaman, form, modal, dan tabel harus pas di viewport mobile (min 320px seperti iPhone SE) hingga desktop lebar tanpa horizontal scroll pada body (`overflow-x-hidden`). Gunakan drawer (`Sheet`) untuk navigasi mobile, tombol aksi touch target minimum 44px, grid adaptif (1 kolom di mobile, 2-3 di desktop), dan scroll horizontal atau tumpukan kartu untuk tabel data (spec §4.4).
- **Izin lewat `can()`** (`src/lib/features/org/permissions.ts`), bukan `role === 'owner'` di komponen. Backend tetap otoritas; UI hanya menyembunyikan.
- **`orgId` hanya dari parameter route** (`page.params.orgId`).
- **Setiap halaman punya keadaan memuat, error, kosong, dan berisi.** Pakai `PageSkeleton`, `ErrorState`, `EmptyState`.
- **Form memakai `createForm`** (`src/lib/forms/form.svelte.ts`); error API lewat `errorMessage()`.
- **Status bayar tidak pernah dari redirect gateway**; selalu dari API (spec §8.4).
- Access token hanya di memori; `localStorage` hanya untuk preferensi (tema, bahasa, `lastOrgId`).
- TypeScript strict: dilarang `any`, `@ts-ignore`; `@ts-expect-error` hanya dengan alasan.
- Fungsi murni yang menentukan perilaku (izin, tombol plan, banner, formatter, pemetaan error) wajib punya unit test Vitest di file `*.test.ts` sebelahnya.

## Pola file

- Route tipis: `src/routes/(public)/...` untuk halaman tanpa login, `src/routes/(app)/...` untuk halaman dengan sesi.
- Fitur: `src/lib/features/<fitur>/{api.ts, schemas.ts, components/, *.test.ts}`.
- Komponen bersama aplikasi: `src/lib/components/app/`. Komponen shadcn: `src/lib/components/ui/` (boleh diedit, jangan diubah warnanya ke nilai mentah).
- Nama file `kebab-case.ts`, komponen `PascalCase.svelte`, modul ber-rune di luar komponen `*.svelte.ts`.
- Halaman form contoh untuk ditiru: login (`routes/(public)/login`, W3.1) dan profil (`routes/(app)/account`, W4.1).

## Batas lingkup

- Jangan mengubah kode backend dari tugas frontend. Bila API kurang atau berbeda dari spec, catat di `web/docs/todo.md` bagian "Catatan untuk Backend" dan sesuaikan frontend dengan perilaku backend yang ada.
- Fitur di luar cakupan (spec §13) jangan dikerjakan; catat di backlog.

## Git

Conventional Commits dengan scope `web`, mis. `feat(web): add login page`, `test(web): cover plan actions`. Aturan lain sama dengan `CLAUDE.md` root.
