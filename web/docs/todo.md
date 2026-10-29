# TODO — Frontend Web

> Rujukan: [spec.md](spec.md) · [plan.md](plan.md) · [../CLAUDE.md](../CLAUDE.md)
> Format: `- [ ] tugas (estimasi hari)`; sub-butir **File**, **Lakukan**, **Selesai bila**. `§` = bagian di [spec.md](spec.md).
> Setiap tugas: kerjakan satu, jalankan `make web-check`, centang, commit (`feat(web): ...`).
> Nama kode (variabel, fungsi, file, komponen, kunci i18n) dalam **bahasa Inggris**; teks UI lewat i18n.

## Ringkasan Durasi

| Fase | Nama | Durasi |
|---|---|---|
| W0 | Setup proyek | 2 hari |
| W1 | Fondasi API & sesi | 3 hari |
| W2 | App shell & komponen bersama | 2 hari |
| W3 | Auth publik & tautan email | 2,5 hari |
| W4 | Akun | 1,5 hari |
| W5 | Organisasi, anggota & undangan | 3 hari |
| W6 | Billing | 3 hari |
| W7 | Penyelesaian & acceptance | 2 hari |
| | **Total** | **19 hari + 2 buffer** |

---

## W0 — Setup Proyek (2 hari)

- [x] **W0.1 Scaffold SvelteKit** (0,25)
  - **Lakukan:** dari root repo jalankan `pnpm dlx sv create web` (template minimal, TypeScript). Lalu `pnpm dlx sv add` untuk `tailwindcss`, `eslint`, `prettier`, `vitest`, `playwright`, `paraglide` (locale `id,en`). Isi `packageManager` di `web/package.json` dengan versi pnpm yang dipakai. Tambahkan `"engines": { "node": ">=22" }` dan file `web/.nvmrc` berisi `22`.
  - **Selesai bila:** `pnpm --dir web dev` menampilkan halaman awal; `pnpm-lock.yaml` di-commit; `node_modules`, `.svelte-kit`, `build`, `src/lib/paraglide` ada di `web/.gitignore`.
- [x] **W0.2 Mode SPA statis** (0,25)
  - **File:** `web/svelte.config.js`, `web/src/routes/+layout.ts`
  - **Lakukan:** pasang `@sveltejs/adapter-static` dengan `fallback: 'index.html'`. Di `+layout.ts` root: `export const ssr = false; export const prerender = false;`. Cek dokumentasi SvelteKit versi terpasang untuk opsi SPA yang benar.
  - **Selesai bila:** `pnpm build` menghasilkan `web/build/index.html`; membuka path acak di `pnpm preview` tetap memuat aplikasi.
- [x] **W0.3 Dev server dan proxy API** (0,1)
  - **File:** `web/vite.config.ts`
  - **Lakukan:** `server.port = 3000`, `strictPort: true`, `server.proxy['/api'] = { target: 'http://localhost:8080', changeOrigin: false }`. Sama untuk `preview`.
  - **Selesai bila:** dengan backend jalan (`make up migrate run`), `curl http://localhost:3000/api/v1/healthz` menjawab OK.
- [x] **W0.4 TypeScript strict dan lint** (0,25)
  - **File:** `web/tsconfig.json`, `web/eslint.config.js`, `web/.prettierrc`
  - **Lakukan:** `strict: true`, `noUncheckedIndexedAccess: true`. ESLint: aturan `@typescript-eslint/no-explicit-any: error`, `@typescript-eslint/ban-ts-comment` (izinkan `ts-expect-error` dengan deskripsi). Prettier dengan `prettier-plugin-svelte` dan `prettier-plugin-tailwindcss`. Abaikan `src/lib/api/schema.d.ts` dan `src/lib/paraglide` di lint/format.
  - **Selesai bila:** `pnpm lint` dan `pnpm check` lulus tanpa warning.
- [x] **W0.5 Script package.json** (0,1)
  - **Lakukan:** pastikan script: `dev`, `build`, `preview`, `check` (`svelte-kit sync && svelte-check --fail-on-warnings`), `lint` (`prettier --check . && eslint .`), `format`, `test` (`vitest run`), `test:e2e` (`playwright test`), `api:generate` (lihat W1.1).
  - **Selesai bila:** semua script jalan (boleh gagal hanya `api:generate` sampai W1.1).
- [x] **W0.6 Design token dan dark mode** (0,5)
  - **File:** `web/src/app.css`, `web/src/routes/+layout.svelte`
  - **Lakukan:** inisialisasi shadcn-svelte (`pnpm dlx shadcn-svelte@latest init`). Ganti nilai variabel di `:root` dan `.dark` dengan tabel §4.1 **persis** (skema putih & biru untuk light mode, serta hitam & biru dark/midnight untuk dark mode, menghindari tema ungu AI generik). Tambah token `--success`, `--success-muted`, `--warning`, `--warning-muted`, `--danger-muted`, `--info`, `--info-muted` dan daftarkan di blok `@theme inline` (`--color-success: var(--success);` dst.). Pasang `@fontsource-variable/inter` dan set `font-family` dasar. Pasang `mode-watcher` (`<ModeWatcher />` di layout root, default `system`).
  - **Selesai bila:** halaman uji sementara dengan `bg-primary`, `text-success`, `bg-warning-muted` tampil benar di light dan dark (ganti lewat `setMode`); halaman uji dihapus sebelum commit.
- [x] **W0.7 Komponen shadcn-svelte awal dan icon SVG** (0,25)
  - **Lakukan:** pasang `lucide-svelte` (atau icon flaticon/tabler SVG) dan jalankan `pnpm dlx shadcn-svelte@latest add button input label card dialog alert-dialog dropdown-menu select table badge avatar separator skeleton sonner sheet tabs progress checkbox alert tooltip sidebar`. Pasang `<Toaster />` di layout root. **Aturan wajib:** dilarang keras memakai emotikon/emoji di UI (gunakan icon vektor SVG dari `lucide-svelte`).
  - **Selesai bila:** komponen ada di `src/lib/components/ui/`, `lucide-svelte` terpasang, `pnpm check` lulus.
- [x] **W0.8 i18n dasar** (0,25)
  - **File:** `web/project.inlang/settings.json`, `web/messages/id.json`, `web/messages/en.json`, `web/src/lib/i18n/index.ts`
  - **Lakukan:** `baseLocale: "id"`, `locales: ["id", "en"]`. Strategi Paraglide `["localStorage", "preferredLanguage", "baseLocale"]` (§10). Tambah kunci `common_app_name`, `common_save`, `common_cancel`, `common_loading`. `lib/i18n/index.ts` mengekspor `getLocale`, `setLocale`, `locales`, dan `intlLocale()` (`id` → `id-ID`, `en` → `en-US`).
  - **Selesai bila:** `import { m } from '$lib/paraglide/messages'` lalu `m.common_save()` tampil sesuai bahasa; mengganti bahasa tidak me-reload halaman.
- [x] **W0.9 Target Makefile root** (0,1)
  - **File:** `Makefile` (root)
  - **Lakukan:** tambah target dengan komentar `##` seperti target lain: `web-install` (`pnpm --dir web install --frozen-lockfile`), `web-dev`, `web-build`, `web-test`, `web-lint`, `web-api` (`pnpm --dir web api:generate`), `web-e2e`, `web-check` (lint + check + test + build berurutan).
  - **Selesai bila:** `make help` menampilkan target baru; `make web-check` hijau.
- [x] **W0.10 CI frontend** (0,25)
  - **File:** `.github/workflows/web.yml`
  - **Lakukan:** job dipicu perubahan `web/**` atau `docs/openapi.yaml`. Langkah: checkout, setup pnpm + Node 22 (cache pnpm), `pnpm install --frozen-lockfile`, `pnpm lint`, `pnpm check`, `pnpm test`, `pnpm build`, lalu `pnpm api:generate && git diff --exit-code src/lib/api/schema.d.ts` (aktifkan setelah W1.1).
  - **Selesai bila:** CI hijau di branch.

**Selesai bila (W0):** `make web-check` hijau, tema light/dark dan bahasa id/en bekerja, CI frontend berjalan.

---

## W1 — Fondasi API & Sesi (3 hari)

- [x] **W1.1 Generate tipe dari OpenAPI** (0,25)
  - **File:** `web/package.json` (script), `web/src/lib/api/schema.d.ts` (hasil), `web/src/lib/api/types.ts`
  - **Lakukan:** script `api:generate` = `openapi-typescript ../docs/openapi.yaml -o src/lib/api/schema.d.ts`. `types.ts` berisi alias: `Profile`, `AuthUser`, `OrganizationRef`, `MemberOrganization`, `Member`, `Invitation`, `InvitationInfo`, `Plan`, `Subscription`, `SubscriptionStatus`, `Invoice`, `Session`, `Role`, `ErrorCode`, `CheckoutResponse`, `ChangePlanResponse` (bentuk `components['schemas']['X']`).
  - **Selesai bila:** `schema.d.ts` di-commit; langkah drift di CI aktif.
  - **Prasyarat:** Fase 8 backend (`docs/todo.md`) sudah menerjemahkan `docs/openapi.yaml` ke Bahasa Inggris, supaya komentar JSDoc di `schema.d.ts` berbahasa Inggris. Bila belum, tetap jalan; cukup jalankan ulang `make web-api` setelah Fase 8 (tipe tidak berubah, hanya komentar).
- [x] **W1.2 `ApiError` dan `toApiError`** (0,5)
  - **File:** `web/src/lib/api/errors.ts`, `errors.test.ts`
  - **Lakukan:** kelas `ApiError` persis seperti §7.1. `toApiError(response: Response | null, body: unknown, cause?: unknown): ApiError` menangani: body format `{error:{code,message,details}}`, body bukan JSON (pakai status → `INTERNAL_ERROR`), `response === null` (jaringan → `NETWORK_ERROR`, status 0), header `Retry-After` dan `details.retry_after_seconds`, header `X-Trace-ID` (simpan di properti `traceId`).
  - **Selesai bila:** unit test untuk tiap kasus di atas lulus.
- [x] **W1.3 State sesi** (0,25)
  - **File:** `web/src/lib/auth/session.svelte.ts`
  - **Lakukan:** objek `session` dengan `$state`: `status: SessionStatus`, `accessToken: string | null`, `expiresAt: number | null`, `user: AuthUser | null`. Fungsi `setSession(tokenResponse, now = Date.now())`, `clearSession()`, `isExpiringSoon(now, marginMs = 30_000)`.
  - **Selesai bila:** unit test `setSession`/`clearSession`/`isExpiringSoon`.
- [x] **W1.4 Refresh single-flight + Web Locks** (0,75)
  - **File:** `web/src/lib/auth/refresh.ts`, `refresh.test.ts`
  - **Lakukan:** `refreshSession(): Promise<boolean>` sesuai §6.3: satu promise bersama per tab; di dalam `navigator.locks.request('saka-auth-refresh', ...)` bila tersedia; `fetch(`${API_BASE}/auth/refresh`, { method: 'POST', credentials: 'include' })`; `200` → `setSession`, return `true`; selain itu `clearSession()`, return `false`. Promise bersama di-reset setelah selesai. Dapat diuji dengan menyuntikkan `fetch` dan `locks` (parameter opsional atau fungsi `createRefresher(deps)`).
  - **Selesai bila:** test: dua panggilan bersamaan = satu `fetch`; panggilan setelah selesai = `fetch` baru; tanpa `navigator.locks` tetap bekerja; `401` → sesi dibersihkan.
- [x] **W1.5 API client** (0,5)
  - **File:** `web/src/lib/api/client.ts`, `client.test.ts`
  - **Lakukan:** `createClient<paths>({ baseUrl: API_BASE, credentials: 'include' })` dari `openapi-fetch`. `API_BASE` = `PUBLIC_API_BASE_URL` (dari `$env/static/public`, default `/api/v1`). Middleware `onRequest`: lewati path publik (`/auth/login`, `/auth/refresh`, `/auth/register`, `/auth/verify-email`, `/auth/forgot-password`, `/auth/reset-password`, `/me/email/confirm`, `/plans`, `/invitations/{token}` GET dan decline); selain itu refresh proaktif bila `isExpiringSoon`, lalu pasang `Authorization: Bearer`. Helper `call()` yang membungkus panggilan: bila respons `401 TOKEN_EXPIRED` → `refreshSession()` lalu ulang sekali; error → lempar `ApiError`; sukses → kembalikan `data` bertipe. Bila sesi berakhir, panggil callback `onSessionEnded` (diset oleh layout, §6.3).
  - **Selesai bila:** unit test (fetch tiruan): Bearer terpasang; `TOKEN_EXPIRED` → refresh lalu retry sekali; gagal dua kali → `ApiError` dan `onSessionEnded` terpanggil; endpoint publik tanpa Bearer.
- [x] **W1.6 Query client dan query key** (0,25)
  - **File:** `web/src/lib/api/query-keys.ts`, `web/src/lib/api/query-client.ts`, `web/src/routes/+layout.svelte`
  - **Lakukan:** `queryKeys` persis contoh §3.2. `QueryClient` dengan `staleTime: 30_000`, `retry` hanya untuk `NETWORK_ERROR` (maks 2), tanpa retry untuk 4xx. Pasang `QueryClientProvider` di layout root.
  - **Selesai bila:** `pnpm check` lulus.
- [x] **W1.7 Pesan error terjemahan** (0,25)
  - **File:** `web/src/lib/api/error-message.ts`, `error-message.test.ts`, `messages/*.json`
  - **Lakukan:** `errorMessage(err: unknown): string` sesuai tabel §7.1 dan §7.3 (kunci i18n `error_<code_lower>` dan `error_reason_<reason>`). Untuk `RATE_LIMITED` sertakan durasi (menit dibulatkan ke atas, atau detik bila < 60).
  - **Selesai bila:** test tiap kode dan reason menghasilkan kunci yang benar.
- [x] **W1.8 Helper form** (0,5)
  - **File:** `web/src/lib/forms/form.svelte.ts`, `web/src/lib/forms/field-errors.ts`, test untuk keduanya
  - **Lakukan:** `createForm({ schema, initial, onSubmit })` sesuai §7.2 (nilai `$state`, `errors`, `formError`, `submitting`, `submit(event)`, `reset()`, `setFieldError(field, message)`). `fieldErrorMessage(field, reason)` memetakan tabel §7.2. Fokus ke field error pertama memakai `document.querySelector('[name="<field>"]')`.
  - **Selesai bila:** test: Zod gagal → tidak memanggil `onSubmit`; `VALIDATION_ERROR` → `errors` terisi; reason tidak dikenal → pesan umum; error lain → `formError`; `submitting` true selama proses.
- [x] **W1.9 Formatter** (0,25)
  - **File:** `web/src/lib/format/money.ts`, `date.ts`, test
  - **Lakukan:** `formatMoney(amount, currency, locale)`, `formatDate(iso, locale)`, `formatDateTime(iso, locale)`, `formatRelative(iso, now, locale)` (pakai `Intl.RelativeTimeFormat`, satuan hari/jam/menit).
  - **Selesai bila:** test: `formatMoney(99000, 'IDR', 'id-ID')` mengandung `99.000`; tanpa desimal; relatif "dalam 3 hari" / "in 3 days".
- [x] **W1.10 Test kesamaan kunci i18n** (0,1)
  - **File:** `web/src/lib/i18n/messages.test.ts`
  - **Selesai bila:** test gagal bila `id.json` dan `en.json` punya kunci berbeda (sebutkan kunci yang hilang).

**Selesai bila (W1):** semua unit test W1 lulus; belum ada halaman.

---

## W2 — App Shell & Komponen Bersama (2 hari)

- [x] **W2.1 Struktur route dan guard** (0,25)
  - **File:** `web/src/routes/(public)/+layout.svelte`, `web/src/routes/(app)/+layout.svelte`, `web/src/lib/auth/guards.ts`
  - **Lakukan:** `ensureSession()` (§6.2). Layout `(app)`: tampilkan layar memuat selama `unknown`; `anonymous` → `goto('/login?redirect=' + encodeURIComponent(path))`. Set `onSessionEnded` client → bersihkan cache query, toast "Sesi berakhir", ke `/login`. Helper `safeRedirect(value): string` (hanya path internal, §11.5) dengan unit test.
  - **Selesai bila:** membuka `/orgs` tanpa login berakhir di `/login?redirect=%2Forgs`.
- [x] **W2.2 Layout publik** (0,25)
  - **File:** `web/src/routes/(public)/+layout.svelte`, `web/src/lib/components/app/AuthCard.svelte`
  - **Lakukan:** kartu tengah (§4.4) dengan slot judul, deskripsi, isi, footer; pemilih bahasa dan tema di pojok kanan atas.
- [x] **W2.3 Pemilih tema dan bahasa** (0,25)
  - **File:** `web/src/lib/components/app/ThemeSwitcher.svelte`, `LanguageSwitcher.svelte`
  - **Selesai bila:** keduanya bertahan setelah reload.
- [x] **W2.4 Data `me` dan organisasi aktif** (0,25)
  - **File:** `web/src/lib/features/account/api.ts`, `web/src/lib/features/org/api.ts`
  - **Lakukan:** `createMeQuery()` (`GET /me`), `createOrganizationQuery(orgId)` (`GET /orgs/{orgId}`). Fungsi `lastOrgId` get/set di `localStorage` (dibungkus try/catch).
- [x] **W2.5 App shell responsif: sidebar desktop, drawer mobile, pemilih organisasi, menu user** (0,5)
  - **File:** `web/src/lib/components/app/AppShell.svelte`, `OrgSwitcher.svelte`, `UserMenu.svelte`, `web/src/routes/(app)/orgs/[orgId]/+layout.svelte`
  - **Lakukan:** komponen `sidebar` shadcn-svelte; navigasi §5.3 dengan item aktif; di mobile (<768px) berubah menjadi drawer off-canvas (`Sheet`) dengan backdrop overlay redup, tombol close SVG, dan auto-close saat item ditekan; touch target tombol & navigasi minimum 44px (§4.4). `OrgSwitcher` dari `me.organizations`. `UserMenu`: nama/email, Akun, Tema, Bahasa, Keluar (logout §6.4 termasuk `BroadcastChannel`). Layout `orgs/[orgId]` mengambil organisasi; `404` → `ErrorState` "Organisasi tidak ditemukan".
  - **Selesai bila:** navigasi dan layout bekerja mulus tanpa overflow horizontal di desktop, tablet, dan mobile (320px–375px).
- [x] **W2.6 Komponen keadaan** (0,25)
  - **File:** `web/src/lib/components/app/EmptyState.svelte`, `ErrorState.svelte`, `PageHeader.svelte`, `PageSkeleton.svelte`, `LoadMore.svelte`
  - **Lakukan:** `ErrorState` menerima `error: unknown`, memakai `errorMessage`, menampilkan `traceId` untuk `INTERNAL_ERROR`, dan tombol "Coba lagi" opsional.
- [x] **W2.7 `StatusBadge`, `RoleBadge`, `ConfirmDialog`** (0,25)
  - **File:** `web/src/lib/components/app/StatusBadge.svelte`, `RoleBadge.svelte`, `ConfirmDialog.svelte`
  - **Lakukan:** `StatusBadge` menerima `kind: 'subscription' | 'invoice'` dan `status`, memetakan sesuai §4.3. `ConfirmDialog`: judul, deskripsi, label tombol, varian `destructive`, opsi `confirmText` (wajib mengetik teks tertentu, dipakai hapus organisasi), fokus awal tombol Batal.
- [x] **W2.8 Banner global** (0,25)
  - **File:** `web/src/lib/components/app/Banner.svelte`, `EmailVerificationBanner.svelte`
  - **Lakukan:** `Banner` varian `info|warning|neutral` dengan slot aksi. `EmailVerificationBanner` sesuai §6.5 (`POST /auth/resend-verification`).

**Selesai bila (W2):** setelah login manual (sementara lewat `curl` atau halaman login W3.1), aplikasi menampilkan shell, pemilih organisasi, dan halaman kosong per menu.

---

## W3 — Auth Publik & Tautan Email (2,5 hari)

Pola halaman form: `+page.svelte` di `routes/(public)/<path>/`, skema Zod di `features/auth/schemas.ts`, panggilan API di `features/auth/api.ts`. **W3.1 menjadi contoh yang ditiru halaman form lain.**

- [x] **W3.1 Login** (0,5) — §6.4
  - **File:** `routes/(public)/login/+page.svelte`, `features/auth/schemas.ts` (`loginSchema`), `features/auth/api.ts` (`login()`)
  - **Selesai bila:** login berhasil ke `redirect` atau `/`; `401`, `403 account_*`, `429` menampilkan pesan §6.4; sudah login → langsung dialihkan.
- [x] **W3.2 Registrasi + cek email** (0,5) — §6.4
  - **File:** `routes/(public)/register/+page.svelte`, `routes/(public)/register/check-email/+page.svelte`
  - **Lakukan:** termasuk mode `?invitation=` (email terkunci, `organization_name` tersembunyi dan diisi nama user).
  - **Selesai bila:** `409` di field email; validasi password (8–72, tidak sama dengan email) di klien.
- [x] **W3.3 Lupa dan reset password** (0,25)
  - **File:** `routes/(public)/forgot-password/+page.svelte`, `routes/(public)/reset-password/+page.svelte`
- [x] **W3.4 Verifikasi email dan konfirmasi ganti email** (0,25)
  - **File:** `routes/(public)/verify-email/+page.svelte`, `routes/(public)/confirm-email-change/+page.svelte`, komponen bersama `features/auth/components/TokenActionPage.svelte` (panggil sekali saat mount, tampilkan memuat/sukses/gagal)
  - **Selesai bila:** request hanya terkirim satu kali walau komponen dirender ulang.
- [x] **W3.5 Terima undangan** (0,5) — §6.4 langkah 1–6
  - **File:** `routes/(public)/accept-invitation/+page.svelte`, `features/invitations/api.ts`
  - **Selesai bila:** semua cabang (belum login + akun ada/tidak, email beda, belum terverifikasi, berhasil, tolak, tautan tidak berlaku per `reason`) bisa dicoba manual.
- [x] **W3.6 Logout dan sinkron antar tab** (0,25)
  - **File:** `web/src/lib/auth/logout.ts`, `web/src/lib/auth/broadcast.ts`
  - **Selesai bila:** logout di satu tab membuat tab lain kembali ke `/login`.
- [x] **W3.7 Redirect root** (0,25)
  - **File:** `routes/(app)/+page.svelte`
  - **Lakukan:** `/` → `lastOrgId` (bila masih ada di `me.organizations`) → organisasi pertama → `/orgs/new`.

**Selesai bila (W3):** registrasi → klik tautan di Mailpit → login → masuk dashboard kosong, seluruhnya dari browser.

---

## W4 — Akun (1,5 hari) — §9.4

Halaman akun memakai sub-navigasi (tab) Profil / Keamanan / Sesi / Hapus akun di `routes/(app)/account/+layout.svelte`.

- [x] **W4.1 Profil** (0,25)
  - **File:** `routes/(app)/account/+page.svelte`, `features/account/schemas.ts`, `features/account/api.ts`, `components/app/UserAvatar.svelte` (fallback inisial, hanya URL `http(s)`)
  - **Lakukan:** kirim hanya field yang berubah; avatar kosong → `null`. Sukses → invalidate `me`, toast.
- [x] **W4.2 Ganti password dan ganti email** (0,5)
  - **File:** `routes/(app)/account/security/+page.svelte`, `features/account/components/ChangePasswordForm.svelte`, `ChangeEmailForm.svelte`
- [x] **W4.3 Sesi** (0,5)
  - **File:** `routes/(app)/account/sessions/+page.svelte`, `features/account/user-agent.ts` (`describeUserAgent()` + test: Chrome/Firefox/Safari/Edge × Windows/macOS/Linux/Android/iOS, fallback)
  - **Selesai bila:** cabut sesi lain menghilangkan baris; cabut sesi ini = logout.
- [x] **W4.4 Hapus akun** (0,25)
  - **File:** `routes/(app)/account/delete/+page.svelte`
  - **Selesai bila:** `409 owns_shared_organization` menampilkan pesan §7.3 + tautan; sukses → `/login` dengan toast.

**Selesai bila (W4):** semua aksi akun bisa dilakukan dari UI.

---

## W5 — Organisasi, Anggota & Undangan (3 hari) — §9.1–§9.3, §9.5

- [x] **W5.1 `can()`** (0,25)
  - **File:** `features/org/permissions.ts`, `permissions.test.ts`
  - **Selesai bila:** test mencakup setiap sel matriks §9.1 termasuk target owner/admin/member dan diri sendiri.
- [x] **W5.2 Daftar dan buat organisasi** (0,5)
  - **File:** `routes/(app)/orgs/+page.svelte`, `routes/(app)/orgs/new/+page.svelte`, `features/org/schemas.ts` (`slugSchema`: `^[a-z0-9]+(-[a-z0-9]+)*$`, 3–48)
- [x] **W5.3 Pengaturan umum organisasi** (0,25)
  - **File:** `routes/(app)/orgs/[orgId]/settings/+page.svelte`, `features/org/components/OrganizationGeneralForm.svelte`
  - **Lakukan:** read-only untuk role tanpa `org.update`.
- [x] **W5.4 Transfer, keluar, hapus organisasi** (0,5)
  - **File:** `features/org/components/TransferOwnershipCard.svelte`, `LeaveOrganizationCard.svelte`, `DeleteOrganizationCard.svelte` (dipasang di halaman settings sesuai `can()`)
  - **Selesai bila:** hapus butuh mengetik nama organisasi; setelah keluar/hapus, `lastOrgId` dibersihkan dan user diarahkan ke `/`.
- [x] **W5.5 Daftar anggota** (0,5)
  - **File:** `routes/(app)/orgs/[orgId]/members/+page.svelte`, `features/members/api.ts` (`createMembersQuery` infinite, `changeRole`, `removeMember`), `features/members/components/MemberRow.svelte`, `MemberUsage.svelte`
- [x] **W5.6 Undangan** (0,5)
  - **File:** `routes/(app)/orgs/[orgId]/members/invitations/+page.svelte`, `features/invitations/components/InviteDialog.svelte`, `InvitationRow.svelte`
  - **Selesai bila:** `201`/`200`/`409`/`402`/`403 EMAIL_NOT_VERIFIED` ditangani sesuai §9.3.
- [x] **W5.7 Dialog upgrade** (0,25)
  - **File:** `web/src/lib/components/app/UpgradeDialog.svelte`, `web/src/lib/features/billing/upgrade.svelte.ts` (`showUpgrade(err: ApiError)`)
  - **Lakukan:** dipanggil untuk `PLAN_LIMIT_REACHED` dan `PLAN_FEATURE_UNAVAILABLE` (§7.1).
- [x] **W5.8 Dashboard ringkasan** (0,25)
  - **File:** `routes/(app)/orgs/[orgId]/+page.svelte`, `features/org/components/*Card.svelte`
  - **Lakukan:** kartu §9.5 (kartu invoice ditambahkan setelah W6.1 bila belum ada query invoice).

**Selesai bila (W5):** alur undang → terima → ubah role → keluarkan bisa dilakukan dari dua browser; mengundang orang ke-4 di plan Free memunculkan dialog upgrade.

---

## W6 — Billing (3 hari) — §8

- [x] **W6.1 API billing** (0,25)
  - **File:** `features/billing/api.ts`
  - **Lakukan:** `createPlansQuery`, `createSubscriptionQuery(orgId)`, `createInvoicesQuery(orgId)` (infinite), `createInvoiceQuery(orgId, id)`, mutation `checkout`, `changePlan`, `cancelSubscription`, `resumeSubscription`; semua mutation meng-invalidate `subscription` dan `invoices`.
- [x] **W6.2 `resolvePlanAction()`** (0,25)
  - **File:** `features/billing/plan-actions.ts`, `plan-actions.test.ts`
  - **Selesai bila:** satu test per baris tabel §8.2 (termasuk urutan pengecekan).
- [x] **W6.3 `subscriptionBanner()` + pemasangan banner** (0,25)
  - **File:** `features/billing/banners.ts`, `banners.test.ts`, `features/billing/components/SubscriptionBanner.svelte` (dipasang di layout `orgs/[orgId]`)
  - **Selesai bila:** satu test per baris tabel §8.5; non-owner tanpa tombol.
- [x] **W6.4 Halaman billing** (0,5) — §8.7
  - **File:** `routes/(app)/orgs/[orgId]/billing/+layout.svelte` (tab Ringkasan / Plan / Invoice), `+page.svelte`, `features/billing/components/UsageMeter.svelte`
- [x] **W6.5 Halaman plan dan alur checkout/ganti plan** (0,75) — §8.2, §8.3
  - **File:** `routes/(app)/orgs/[orgId]/billing/plans/+page.svelte`, `features/billing/components/PlanCard.svelte`, `features/billing/purchase.ts` (`handlePurchaseResult(result)` dan `goToPayment({ orgId, invoice })`)
  - **Selesai bila:** trial, jadwal downgrade + warnings, batal downgrade, dan redirect ke `payment_url` bekerja.
- [x] **W6.6 Simulasi pembayaran dev** (0,25) — §8.6
  - **File:** `web/src/lib/dev/fake-payment.ts`, `features/billing/components/FakePaymentDialog.svelte`
  - **Lakukan:** HMAC-SHA256 lewat `crypto.subtle`; body `{event_id, invoice_number, provider_ref, state: "paid", amount, currency}` (lihat `docs/demo.md` Langkah 5). Import dinamis hanya di dalam `if (import.meta.env.DEV)`.
  - **Selesai bila:** di dev, checkout Pro → bayar simulasi → status `active`; `pnpm build && grep -r "fake-webhook-secret" build/` kosong.
- [x] **W6.7 Halaman kembali dari pembayaran** (0,25) — §8.4
  - **File:** `routes/(public)/billing/return/+page.svelte`
  - **Selesai bila:** polling berhenti saat `paid`, waktu habis menampilkan pesan "sedang diproses", tanpa `saka.pendingPayment` diarahkan ke `/`.
- [x] **W6.8 Invoice** (0,5)
  - **File:** `routes/(app)/orgs/[orgId]/billing/invoices/+page.svelte`, `invoices/[invoiceId]/+page.svelte`
  - **Lakukan:** termasuk kartu "Invoice belum dibayar" di dashboard (W5.8).

**Selesai bila (W6):** dengan provider `fake`, alur Free → trial Pro → bayar → active → jadwalkan downgrade → batalkan → batal langganan → lanjutkan bisa dilakukan dari UI.

---

## W7 — Penyelesaian & Acceptance (2 hari)

- [x] **W7.1 Helper E2E** (0,25)
  - **File:** `web/tests/e2e/helpers/mailpit.ts` (ambil email terbaru untuk alamat tertentu lewat `http://localhost:8025/api/v1/search?query=to:<email>` lalu ekstrak tautan), `helpers/users.ts` (email unik per test), `playwright.config.ts` (`baseURL: http://localhost:3000`, `webServer` menjalankan `pnpm dev`).
- [x] **W7.2 Skenario E2E §11.2 nomor 1–4** (0,5)
- [x] **W7.3 Skenario E2E §11.2 nomor 5–8** (0,5)
- [x] **W7.4 Audit teks, warna, dan icon** (0,25)
  - **Lakukan:** pastikan tidak ada teks UI hardcode (cek manual tiap `.svelte` di `routes` dan `features`), tidak ada emotikon/emoji di komponen (hanya SVG icon), dan `grep -rnE "(bg|text|border)-(red|green|blue|indigo|zinc|gray|slate|amber|emerald)-[0-9]" src/routes src/lib/features src/lib/components/app` kosong.
- [x] **W7.5 Audit aksesibilitas dan responsivitas (mobile & desktop)** (0,25) — §4.4, §11.4
  - **Lakukan:** navigasi seluruh alur dengan keyboard saja; cek label, fokus dialog, `aria-label` ikon. Uji audit responsivitas pada viewport 320px (iPhone SE), 375px/390px, 768px (tablet), dan 1280px+ (desktop): pastikan *zero horizontal overflow* pada body, tabel tidak terpotong (tersedia scroll horizontal atau mode tumpuk), touch target minimum 44px di mobile, modal dialog pas di viewport, dan banner membungkus rapi.
- [x] **W7.6 Panduan deploy dan README** (0,25)
  - **File:** `web/docs/deploy.md` (contoh Nginx/Caddy same-origin dengan proxy `/api`, fallback SPA ke `index.html`, header keamanan termasuk CSP, varian subdomain dengan `PUBLIC_API_BASE_URL` + `app.cors_allowed_origins`), `web/README.md` (setup singkat, perintah), tautan dari `README.md` root.
- [x] **W7.7 Centang kriteria selesai §12 spec** (0,1)

**Selesai bila (W7):** semua kotak §12 spec tercentang.

---

## Tugas Manual Pemilik Repo

- [ ] Atur Finish/Unfinish/Error Redirect URL di dashboard Midtrans (sandbox, lalu production) ke `<frontend_url>/billing/return` (§8.4). Dikerjakan bersamaan dengan verifikasi sandbox Midtrans backend.
- [ ] Tentukan domain production (same-origin disarankan) dan isi `app.frontend_url` serta `app.cors_allowed_origins` backend.

## Catatan untuk Backend

Hal yang ditemukan saat merancang frontend. **Bukan** bagian pekerjaan frontend; usulkan ke backlog backend (`docs/todo.md`) bila disetujui.

- `POST /auth/register` mewajibkan `organization_name` walaupun email punya undangan aktif (organisasi tidak dibuat). Usulan: jadikan opsional bila ada undangan aktif. Sementara frontend mengisi otomatis (spec §6.4).
- `message` (error dan sukses) dan isi email akan berbahasa Inggris lewat Fase 8 backend (`docs/todo.md`); path, status, `code`, dan `details` tidak berubah, jadi frontend tidak perlu menyesuaikan apa pun selain menganggap `serverMessage` berbahasa Inggris.
- Pesan `details.fields` pada `VALIDATION_ERROR` berupa teks Inggris teknis, bukan kode. Usulan jangka panjang: kode alasan yang stabil per field (mis. `incorrect`, `required`) supaya frontend tidak bergantung pada teks.

## Backlog (di luar versi pertama)

- Upload avatar/logo (butuh backend upload file).
- Halaman audit log (fitur `audit_log` plan Business) dan export CSV (`export_csv`), menunggu backend.
- 2FA, login OAuth, panel super admin, notifikasi in-app (mengikuti Tier 2 backend).
- E2E di CI (butuh backend + PostgreSQL + Redis + Mailpit di workflow).
- Landing page marketing (proyek terpisah, mis. Astro).
