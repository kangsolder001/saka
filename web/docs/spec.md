# Spesifikasi Frontend Web — Saka

> Versi: Draft v0.1 · Rujukan backend: [../../docs/spec.md](../../docs/spec.md) · Kontrak API: [../../docs/openapi.yaml](../../docs/openapi.yaml)
> Rencana: [plan.md](plan.md) · Checklist: [todo.md](todo.md) · Aturan kerja agent: [../CLAUDE.md](../CLAUDE.md)

Dokumen ini adalah sumber kebenaran untuk frontend web. Bila ada yang bertentangan dengan backend, **backend dan `openapi.yaml` yang benar**; frontend menyesuaikan dan perbedaannya dicatat di [todo.md](todo.md) bagian "Catatan untuk backend".

---

## 1. Tujuan

Aplikasi web (dashboard) untuk boilerplate SaaS Saka. Semua fitur backend Tier 1 punya antarmuka: auth, akun, organisasi, anggota dan undangan, billing. Frontend ini juga boilerplate: produk turunan mengganti brand lewat design token dan menambah halaman domain di atasnya.

### Prinsip

1. **Backend adalah otoritas.** Frontend menyembunyikan tombol yang tidak boleh dipakai role tertentu demi kenyamanan, tetapi tidak pernah menganggap itu pengaman. Status bayar, role, dan limit selalu dibaca dari API.
2. **Satu jalur ke API.** Semua request lewat `src/lib/api/`. Komponen tidak memanggil `fetch`.
3. **Tipe dari kontrak.** Tipe request/response di-generate dari `docs/openapi.yaml`. Tidak ada tipe API yang ditulis tangan.
4. **Tidak ada teks UI yang di-hardcode.** Semua teks lewat i18n (Indonesia dan Inggris).
5. **Tidak ada warna yang di-hardcode.** Komponen hanya memakai design token (§4).
6. **Setiap layar punya empat keadaan:** memuat, error, kosong, dan berisi.

---

## 2. Stack

Versi di bawah adalah versi terbaru per 2026-10-28; versi pasti dikunci oleh `pnpm-lock.yaml`.

| Kebutuhan | Pilihan | Versi |
|---|---|---|
| Runtime / package manager | Node.js 22 LTS, **pnpm** (dikunci lewat `packageManager` di `package.json`) | – |
| Framework | **SvelteKit** + **Svelte 5 (runes)**, TypeScript strict | Kit 3.x, Svelte 5.x |
| Mode build | **`@sveltejs/adapter-static`**, SPA (`ssr = false`, fallback `index.html`) | 4.x |
| Styling | **Tailwind CSS v4** | 4.x |
| Komponen UI | **shadcn-svelte** (komponen disalin ke `src/lib/components/ui`, berbasis Bits UI) | CLI 1.x, bits-ui 2.x |
| Ikon | `@lucide/svelte` | – |
| Font | Inter, di-host sendiri lewat `@fontsource-variable/inter` (tanpa request ke Google) | – |
| Dark mode | `mode-watcher` (kelas `.dark` di `<html>`) | 1.x |
| Toast | `svelte-sonner` (lewat komponen Sonner shadcn-svelte) | 1.x |
| Data fetching | **`@tanstack/svelte-query`** (API berbasis runes) | 6.x |
| API client | **`openapi-typescript`** (generate tipe) + **`openapi-fetch`** (client bertipe) | 7.x / 0.17.x |
| Validasi form | **Zod** + helper form sendiri (`src/lib/forms`, §7) | 4.x |
| i18n | **Paraglide JS** (inlang) | 2.x |
| Unit test | **Vitest** | 5.x |
| E2E test | **Playwright**, terhadap backend asli lokal | 1.x |
| Lint / format | ESLint (flat config, `eslint-plugin-svelte`), Prettier (`prettier-plugin-svelte`, `prettier-plugin-tailwindcss`), `svelte-check` | – |

**Tidak dipakai (sengaja):** Next.js / SSR (auth sudah lengkap di Go, tidak perlu server Node), `sveltekit-superforms` (dirancang untuk form action di server; di mode SPA helper kecil sendiri lebih mudah dipahami dan dirawat), library tanggal (cukup `Intl`), state manager global (cukup TanStack Query + rune `$state`).

---

## 3. Arsitektur

### 3.1 Struktur folder

```
web/
  CLAUDE.md                 # aturan kerja agent untuk frontend
  docs/                     # spec.md, plan.md, todo.md (Bahasa Indonesia)
  messages/
    id.json                 # teks UI Bahasa Indonesia (bahasa dasar)
    en.json                 # teks UI Bahasa Inggris
  src/
    app.css                 # design token (§4) + import Tailwind
    app.html
    lib/
      api/
        schema.d.ts         # HASIL GENERATE dari ../docs/openapi.yaml, di-commit, jangan diedit
        client.ts           # instance openapi-fetch + middleware auth/error
        errors.ts           # ApiError, toApiError(), errorMessage()
        query-keys.ts       # semua query key TanStack Query
        types.ts            # alias tipe dari schema (type Member = components['schemas']['Member'])
      auth/
        session.svelte.ts   # state sesi: access token di memori, user, status
        refresh.ts          # refresh single-flight + Web Locks
        guards.ts           # requireSession(), redirectIfAuthenticated()
      forms/
        form.svelte.ts      # createForm(): nilai, error per field, submit
        field-errors.ts     # peta (field, reason backend) -> pesan i18n
      format/
        money.ts            # formatMoney(amount, currency, locale)
        date.ts             # formatDate(), formatDateTime(), formatRelative()
      i18n/                 # helper locale (setLocale, getLocale) di atas Paraglide
      paraglide/            # HASIL GENERATE Paraglide, tidak di-commit (di .gitignore)
      components/
        ui/                 # komponen shadcn-svelte (boleh diedit)
        app/                # komponen aplikasi bersama: AppShell, OrgSwitcher, UserMenu,
                            # StatusBadge, EmptyState, ErrorState, PageHeader, ConfirmDialog,
                            # LanguageSwitcher, ThemeSwitcher, Pagination (load more)
      features/
        auth/               # schemas.ts, api.ts, components/
        account/
        org/                # permissions.ts (can()), schemas.ts, api.ts, components/
        members/
        invitations/
        billing/            # plan-actions.ts (resolvePlanAction()), banners.ts, api.ts, components/
      dev/
        fake-payment.ts     # hanya dev: simulasi webhook provider "fake" (§8.6)
    routes/                 # lihat §5
  tests/
    e2e/                    # Playwright
  static/
  package.json, svelte.config.js, vite.config.ts, tsconfig.json, eslint.config.js, .prettierrc, playwright.config.ts
```

Aturan penempatan:
- Kode yang dipakai **satu fitur** ada di `features/<fitur>/`. Kode yang dipakai **dua fitur atau lebih** naik ke `lib/` atau `components/app/`.
- File route (`+page.svelte`) tipis: mengambil parameter, memanggil query/mutation dari `features/*/api.ts`, dan merangkai komponen. Logika bisnis (siapa boleh apa, tombol plan mana yang tampil) ada di fungsi murni di `features/` yang punya unit test.
- Nama file: `kebab-case.ts`, komponen `PascalCase.svelte`, file yang memakai rune di luar komponen `*.svelte.ts`.

### 3.2 Alur data

```
+page.svelte ──▶ features/x/api.ts (createQuery / createMutation)
                     │
                     ▼
               lib/api/client.ts (openapi-fetch, tipe dari schema.d.ts)
                     │  middleware: pasang Bearer, refresh bila perlu, ubah error jadi ApiError
                     ▼
               /api/v1/... (backend Go)
```

- **Query** (GET) memakai `createQuery`. **Mutation** (POST/PATCH/DELETE) memakai `createMutation`, lalu meng-invalidate query key terkait di `onSuccess`.
- Query key hanya didefinisikan di `lib/api/query-keys.ts`, contoh:
  ```ts
  export const queryKeys = {
    me: ['me'] as const,
    plans: ['plans'] as const,
    organization: (orgId: string) => ['orgs', orgId] as const,
    members: (orgId: string) => ['orgs', orgId, 'members'] as const,
    invitations: (orgId: string) => ['orgs', orgId, 'invitations'] as const,
    subscription: (orgId: string) => ['orgs', orgId, 'subscription'] as const,
    invoices: (orgId: string) => ['orgs', orgId, 'invoices'] as const,
    invoice: (orgId: string, invoiceId: string) => ['orgs', orgId, 'invoices', invoiceId] as const,
    sessions: ['me', 'sessions'] as const,
    invitationByToken: (token: string) => ['invitation', token] as const,
  };
  ```
- `staleTime` default 30 detik. Saat logout, seluruh cache query dibersihkan (`queryClient.clear()`).

### 3.3 Paginasi

Semua daftar backend memakai cursor (`?limit=&cursor=`, respons `{items, next_cursor}`). Frontend memakai **`createInfiniteQuery`** dengan tombol **"Muat lebih banyak"** (bukan nomor halaman, karena cursor tidak mendukung lompat halaman). `limit` = 20. Komponen bersama: `components/app/LoadMore.svelte`.

### 3.4 Lingkungan dan koneksi ke API

- **Dev:** Vite dev server di **port 3000** (sama dengan `app.frontend_url` dan `app.cors_allowed_origins` di `config.example.json` backend). Vite mem-proxy `/api` ke `http://localhost:8080`, jadi browser melihat frontend dan API di origin yang sama: cookie refresh (`Path=/api/v1/auth`) bekerja tanpa CORS.
- **Production (disarankan):** file statis hasil build dan API dilayani di **domain yang sama**, reverse proxy (Nginx/Caddy) meneruskan `/api/` ke backend. Ini yang paling sederhana dan aman.
- **Production (alternatif):** frontend dan API di subdomain berbeda dari domain induk yang sama (`app.contoh.com` dan `api.contoh.com`). Set `PUBLIC_API_BASE_URL=https://api.contoh.com/api/v1` saat build dan tambahkan origin frontend ke `app.cors_allowed_origins` backend. Domain induk yang berbeda **tidak didukung** karena cookie `SameSite=Strict`.
- Konfigurasi frontend hanya satu variabel build-time: `PUBLIC_API_BASE_URL` (default `/api/v1`). Aturan "konfigurasi hanya dari `config.json`" berlaku untuk backend; frontend statis memakai variabel build Vite.

---

## 4. Tema dan Design Token

Tema mengusung estetika enterprise profesional dan bersih: **perpaduan warna putih dan biru (Light Mode)** serta **hitam dan biru dark / midnight blue (Dark Mode)**. Pendekatan ini sengaja dipilih untuk menghindari kesan visual "AI generated" (seperti gradien ungu/indigo/violet klise). Semua warna didefinisikan sebagai CSS variable di `src/app.css`. Nama token mengikuti konvensi shadcn-svelte supaya komponen bawaannya langsung cocok, ditambah token status sendiri.

> Hati-hati dengan penamaan shadcn: token `--accent` di shadcn berarti **latar hover netral**, bukan warna brand. Warna brand (biru) ada di `--primary`.

### 4.1 Nilai token

| Token | Fungsi | Light (Putih & Biru) | Dark (Hitam & Biru Dark) |
|---|---|---|---|
| `--background` | latar halaman | `#FFFFFF` | `#030712` |
| `--foreground` | teks utama | `#0F172A` | `#F8FAFC` |
| `--card` / `--card-foreground` | kartu | `#FFFFFF` / `#0F172A` | `#0B132B` / `#F8FAFC` |
| `--popover` / `--popover-foreground` | dropdown, dialog | `#FFFFFF` / `#0F172A` | `#0F172A` / `#F8FAFC` |
| `--primary` / `--primary-foreground` | aksi utama, link, menu aktif | `#1D4ED8` / `#FFFFFF` | `#3B82F6` / `#030712` |
| `--secondary` / `--secondary-foreground` | tombol sekunder | `#F1F5F9` / `#0F172A` | `#1E293B` / `#F8FAFC` |
| `--muted` / `--muted-foreground` | latar redup / teks sekunder | `#F1F5F9` / `#64748B` | `#1E293B` / `#94A3B8` |
| `--accent` / `--accent-foreground` | latar hover netral/biru lembut | `#EFF6FF` / `#1D4ED8` | `#1E293B` / `#60A5FA` |
| `--destructive` / `--destructive-foreground` | bahaya, hapus | `#DC2626` / `#FFFFFF` | `#F87171` / `#030712` |
| `--border` | garis | `#E2E8F0` | `#1E293B` |
| `--input` | garis input | `#E2E8F0` | `#334155` |
| `--ring` | focus ring | `#2563EB` | `#3B82F6` |
| `--sidebar` / `--sidebar-foreground` | sidebar | `#F8FAFC` / `#0F172A` | `#070D1A` / `#F8FAFC` |
| `--sidebar-primary` / `--sidebar-primary-foreground` | item aktif sidebar | `#1D4ED8` / `#FFFFFF` | `#3B82F6` / `#030712` |
| `--sidebar-accent` / `--sidebar-accent-foreground` | hover sidebar | `#EFF6FF` / `#1D4ED8` | `#1E293B` / `#60A5FA` |
| `--sidebar-border` / `--sidebar-ring` | garis sidebar & focus | `#E2E8F0` / `#2563EB` | `#1E293B` / `#3B82F6` |
| `--success` / `--success-muted` | teks / latar sukses | `#047857` / `#ECFDF5` | `#34D399` / `#052E22` |
| `--warning` / `--warning-muted` | teks / latar peringatan | `#B45309` / `#FFFBEB` | `#FBBF24` / `#2D1F05` |
| `--danger-muted` | latar bahaya (teksnya `--destructive`) | `#FEF2F2` | `#2D0B0B` |
| `--info` / `--info-muted` | teks / latar info (aksen biru) | `#1D4ED8` / `#EFF6FF` | `#60A5FA` / `#172554` |
| `--radius` | radius dasar | `0.5rem` | `0.5rem` |

Token tambahan (`success`, `warning`, `danger-muted`, `info`) didaftarkan ke Tailwind lewat `@theme inline` supaya kelas seperti `text-success`, `bg-warning-muted` tersedia.

### 4.2 Aturan pemakaian

- Komponen **hanya** memakai kelas token: `bg-background`, `text-muted-foreground`, `bg-primary`, `text-success`, `bg-warning-muted`, `border-border`, dan seterusnya. **Dilarang** kelas warna mentah seperti `bg-blue-600`, `text-red-500`, `bg-zinc-100`, atau nilai hex di komponen.
- Warna primer (biru) dipakai hemat: satu aksi utama per layar, link, menu aktif, focus ring.
- Kedalaman di dark mode dibuat dengan lapisan (`background` hitam pekat `#030712` lebih gelap dari `card` biru gelap `#0B132B`), bukan bayangan.
- Semua pasangan teks/latar di tabel di atas memenuhi kontras WCAG AA. Jangan membuat kombinasi baru di luar tabel.
- Mode: `light`, `dark`, `system` (default `system`), dipilih di menu user dan di halaman publik. Pilihan disimpan `mode-watcher` di `localStorage`.

### 4.3 Pemetaan status ke warna

Satu komponen `StatusBadge` memetakan status backend ke varian warna:

| Varian | Status |
|---|---|
| `success` | langganan `active`; invoice `paid` |
| `info` | langganan `trialing`; invoice `open` |
| `warning` | langganan `past_due`; langganan dengan `cancel_at_period_end = true` |
| `neutral` (muted) | langganan `expired`, `canceled`; invoice `draft`, `expired`, `void` |
| `danger` | hanya untuk error dan aksi destruktif, tidak untuk status |

Role ditampilkan sebagai badge netral: `owner`, `admin`, `member` (label diterjemahkan).

### 4.4 Tipografi, Layout, dan Desain Responsif (Mobile & Desktop)

Aplikasi dirancang **sepenuhnya responsif** dan nyaman digunakan di berbagai ukuran layar: mulai dari ponsel berlayar sempit (min 320px seperti iPhone SE), tablet (iPad/portrait), hingga desktop monitor lebar (1440px+).

#### 1. Tipografi
- Font Inter (variable).
- Ukuran teks dasar: 14px untuk aplikasi internal (dashboard, pengaturan, tabel), 16px untuk halaman publik (login, register). Teks judul menggunakan skala modular (`text-xl` hingga `text-2xl` di mobile, `text-3xl` di desktop) dengan perataan line-height yang nyaman dibaca.

#### 2. Layout Halaman Publik (Auth, Tautan Email)
- Kartu form berada di tengah layar (`min-h-screen flex items-center justify-center p-4`).
- Lebar maksimum kartu: 400px di desktop; di mobile (`< 640px`) kartu melebar proporsional (`w-full`) dengan padding yang pas agar form tidak tertekan.
- Logo dan judul aplikasi di atas kartu; pemilih tema dan bahasa terletak di sudut kanan atas atau footer form.

#### 3. Layout Aplikasi (App Shell)
- **Desktop (`>= 768px` / `md` breakpoint):**
  - Sidebar tetap (*fixed*) di sisi kiri selebar 240px (`w-60`).
  - Pemilih organisasi di bagian atas sidebar, menu navigasi di tengah, menu profil pengguna di bagian bawah.
  - Konten utama memiliki margin kiri 240px, padding 24px (`p-6`), lebar maksimum konten 1024px (`max-w-5xl`) terpusat rapi di tengah layar.
- **Mobile & Tablet Portrait (`< 768px`):**
  - Header sticky di atas setinggi 56px (`h-14`) dengan tombol hamburger menu (kiri), nama organisasi / breadcrumb ringkas (tengah), dan status / aksi cepat (kanan).
  - Sidebar berubah menjadi **off-canvas drawer (Sheet)** yang meluncur mulus dari sisi kiri dengan **backdrop overlay redup**.
  - Drawer otomatis menutup ketika pengguna menekan tautan navigasi, tombol close (✕ via icon SVG), atau menekan area backdrop luar.
  - Padding konten menjadi 16px (`p-4`), margin kiri dinonaktifkan (`ml-0`), dan lebar kontainer 100% tanpa adanya horizontal scrollbar liar (*no horizontal scroll on body*).

#### 4. Pola Komponen Responsif
- **Grid Kartu:** 1 kolom tumpuk di mobile (`grid-cols-1`), 2 kolom di tablet (`sm:grid-cols-2`), dan 3 kolom di desktop (`lg:grid-cols-3`).
- **Tabel Data (Anggota, Invoice, Sesi):**
  - Selalu dibungkus kontainer scroll horizontal (`overflow-x-auto`) dengan indikator bayangan atau border rapi, ATAU beralih menjadi kartu tumpuk (*stacked cards*) di mobile agar kolom-kolom data penting tidak terpotong.
- **Formulir & Input:**
  - Area sentuh (*touch target*) input, tombol, dan switch minimum setinggi **44px** (mengikuti standar WCAG 2.5.5 Target Size) agar mudah ditekan jari di layar sentuh.
  - Tombol aksi utama pada form (Simpan, Undang, Bayar) tampil selebar kontainer (*full-width* / `w-full`) di mobile dan otomatis kembali proporsional (*inline*) di desktop.
- **Modal & Dialog Konfirmasi:**
  - Di desktop berupa dialog modal di tengah layar (`max-w-lg`). Di layar mobile (`< 640px`) dialog menyesuaikan diri menjadi modal bawah (*bottom drawer/sheet*) atau modal tengah dengan batas tinggi layar maksimum `max-h-[90vh]` dan scroll mandiri di dalam konten dialog.
- **Banner Global:**
  - Teks banner membungkus (*text-wrap*) dengan alami di layar sempit; tombol aksi di dalam banner bertumpuk vertikal (*flex-col*) di mobile dan sejajar (*flex-row*) di desktop.

### 4.5 Aturan Icon dan Larangan Emotikon

- **Dilarang keras memakai emotikon/emoji** di antarmuka pengguna (seperti 🚀, 💡, 🌙, ☀️, ✕, dsb.) pada tombol, status, banner, empty-state, header, atau tabel. Penggunaan emoji membuat aplikasi terkesan tidak profesional dan seperti boilerplate AI generik.
- **Wajib menggunakan Icon Vektor SVG Profesional**:
  - Gunakan pustaka icon vektor seperti **Lucide Svelte** (`lucide-svelte`, standar resmi pasangan shadcn-svelte) atau SVG dari **Flaticon** / **Tabler Icons**.
  - Gaya icon harus konsisten: stroke width 1.5px hingga 2px (outline style).
  - Skala ukuran standar:
    - `14px - 16px`: icon inline di tombol kecil (`btn-sm`), status badge, atau tabel.
    - `18px - 20px`: icon navigasi sidebar dan item menu header.
    - `32px - 48px`: icon ilustratif pada `EmptyState` atau `ErrorState`.
  - Icon yang memiliki arti fungsional wajib diberi atribut aksesibilitas yang tepat (misalnya `aria-hidden="true"` bila sudah ada teks pendamping, atau `aria-label` bila berupa tombol icon saja).

---

## 5. Peta Halaman

Path tautan email **ditentukan backend** dan tidak boleh diubah: `/verify-email`, `/reset-password`, `/confirm-email-change`, `/accept-invitation` (semuanya dengan `?token=`).

### 5.1 Publik (group `(public)`, tanpa sidebar)

| Path | Fungsi | Endpoint |
|---|---|---|
| `/login` | Login. Query `?redirect=` (path internal saja) | `POST /auth/login` |
| `/register` | Registrasi. Query `?invitation=<token>` untuk alur undangan (§6.4) | `POST /auth/register` |
| `/register/check-email` | "Cek email Anda" setelah registrasi | – |
| `/forgot-password` | Minta tautan reset | `POST /auth/forgot-password` |
| `/reset-password?token=` | Password baru | `POST /auth/reset-password` |
| `/verify-email?token=` | Verifikasi otomatis saat dibuka | `POST /auth/verify-email` |
| `/confirm-email-change?token=` | Konfirmasi email baru otomatis saat dibuka | `POST /me/email/confirm` |
| `/accept-invitation?token=` | Detail undangan, terima/tolak (§6.4) | `GET /invitations/{token}`, `POST .../accept`, `POST .../decline` |
| `/billing/return` | Kembali dari halaman bayar gateway (§8.4) | `GET /orgs/{orgId}/invoices/{id}` |

`/accept-invitation` dan `/billing/return` bisa diakses tanpa login, tetapi menyesuaikan diri bila ada sesi.

### 5.2 Aplikasi (group `(app)`, butuh sesi)

| Path | Fungsi | Siapa |
|---|---|---|
| `/` | Arahkan ke organisasi terakhir (`localStorage`), atau organisasi pertama, atau `/orgs/new` bila tidak punya | semua |
| `/orgs` | Daftar organisasi saya | semua |
| `/orgs/new` | Buat organisasi | semua |
| `/orgs/[orgId]` | Dashboard ringkasan (§9) | anggota |
| `/orgs/[orgId]/members` | Daftar anggota, ubah role, keluarkan | anggota (aksi sesuai role) |
| `/orgs/[orgId]/members/invitations` | Undangan aktif, undang, kirim ulang, batalkan | owner, admin |
| `/orgs/[orgId]/settings` | Umum (nama, slug, logo), transfer kepemilikan, keluar, hapus organisasi | anggota (bagian sesuai role) |
| `/orgs/[orgId]/billing` | Status langganan, pemakaian, aksi batal/lanjutkan | anggota (aksi hanya owner) |
| `/orgs/[orgId]/billing/plans` | Perbandingan dan pilih plan | anggota (tombol hanya owner) |
| `/orgs/[orgId]/billing/invoices` | Riwayat invoice | owner |
| `/orgs/[orgId]/billing/invoices/[invoiceId]` | Detail invoice dan tombol bayar | owner |
| `/account` | Profil (nama, avatar) | semua |
| `/account/security` | Ganti password, ganti email | semua |
| `/account/sessions` | Sesi aktif, cabut sesi | semua |
| `/account/delete` | Hapus akun | semua |

Halaman yang hanya untuk role tertentu tetap bisa dibuka lewat URL; bila API menjawab `403`, tampilkan `ErrorState` "Anda tidak punya akses" (bukan error generik).

### 5.3 Navigasi sidebar

Urutan: Ringkasan, Anggota, Billing, Pengaturan. Item "Undangan" berupa tab di halaman Anggota (hanya terlihat owner/admin). "Invoice" berupa tab di halaman Billing (hanya terlihat owner). Menu user (bawah): nama dan email, Akun, Tema, Bahasa, Keluar.

---

## 6. Autentikasi dan Sesi

### 6.1 Penyimpanan token

- **Access token** (JWT 15 menit) hanya disimpan **di memori** (`lib/auth/session.svelte.ts`). Tidak pernah di `localStorage`, `sessionStorage`, atau cookie yang bisa dibaca JS.
- **Refresh token** ada di cookie `HttpOnly` yang dipasang backend (login dengan `token_delivery` default `cookie`). Frontend tidak pernah membaca atau mengirimnya secara manual; browser mengirimnya otomatis ke `/api/v1/auth/*`. Panggil `fetch` refresh dengan `credentials: 'include'` (wajib untuk skenario subdomain, tidak berbahaya di same-origin).
- State sesi:
  ```ts
  type SessionStatus = 'unknown' | 'authenticated' | 'anonymous';
  // session.status, session.accessToken, session.expiresAt (ms), session.user (AuthUser | null)
  ```

### 6.2 Memulai aplikasi (bootstrap)

1. Saat aplikasi dimuat, `session.status = 'unknown'`.
2. Layout `(app)` memanggil `ensureSession()`: bila belum ada access token, panggil `POST /auth/refresh` (tanpa body).
   - `200` → simpan `access_token`, `expires_in`, `user`; status `authenticated`.
   - `401` → status `anonymous`, arahkan ke `/login?redirect=<path sekarang>`.
3. Selama `unknown`, tampilkan layar memuat penuh (logo + spinner), bukan kilasan halaman login.
4. Halaman `/login` dan `/register`: bila `ensureSession()` berhasil, langsung arahkan ke `redirect` atau `/`.

### 6.3 Refresh token (WAJIB benar)

Backend menganggap **dua refresh bersamaan dengan token yang sama sebagai pencurian** dan mencabut seluruh sesi. Karena itu:

1. **Single-flight dalam satu tab:** `refreshSession()` menyimpan satu `Promise` yang sedang berjalan; pemanggil berikutnya menunggu promise yang sama.
2. **Serial antar tab:** proses refresh dibungkus `navigator.locks.request('saka-auth-refresh', ...)` (Web Locks API). Tab lain menunggu sampai tab pertama selesai, lalu memakai cookie yang sudah dirotasi. Bila `navigator.locks` tidak tersedia, jatuh ke single-flight saja.
3. **Kapan refresh:**
   - Proaktif: sebelum request, bila access token kedaluwarsa dalam < 30 detik.
   - Reaktif: bila API menjawab `401` dengan kode `TOKEN_EXPIRED`, refresh lalu **ulangi request sekali**. Request yang gagal lagi tidak diulang.
   - `401 UNAUTHENTICATED` (bukan `TOKEN_EXPIRED`) atau refresh gagal → sesi berakhir: bersihkan state dan cache query, arahkan ke `/login?redirect=...` dengan toast "Sesi Anda berakhir".
4. Request ke `/auth/login`, `/auth/refresh`, `/auth/register`, dan endpoint publik lain tidak memicu refresh.
5. Logika ini punya unit test (§11.1): dua request paralel dengan token kedaluwarsa hanya memicu **satu** panggilan refresh.

### 6.4 Alur per halaman

- **Login:** form `email`, `password`. `401` → "Email atau password salah" (pesan umum, tidak membedakan penyebab). `403` dengan `details.reason` berawalan `account_` → "Akun tidak dapat digunakan". `429` → "Terlalu banyak percobaan, coba lagi dalam N menit" (dari header `Retry-After` atau `details.retry_after_seconds`). Berhasil → `redirect` (hanya path yang diawali `/` dan bukan `//`) atau `/`.
- **Registrasi:** `name`, `email`, `password` (8–72 karakter, tidak sama dengan email), `organization_name`. Berhasil (`201`, tanpa token) → `/register/check-email?email=...`. `409` → error di field `email` ("Email sudah terdaftar").
- **Registrasi dari undangan** (`/register?invitation=<token>`): ambil `GET /invitations/{token}`, isi `email` dari undangan dan kunci (read-only), tampilkan "Anda diundang ke {organisasi}". Field `organization_name` **disembunyikan** dan diisi otomatis dengan nama user (backend mewajibkan field ini tetapi tidak membuat organisasi bila email punya undangan aktif; lihat "Catatan untuk backend" di todo). Setelah registrasi: halaman cek email dengan pesan "Verifikasi email Anda untuk bergabung ke {organisasi}".
- **Verifikasi email** (`/verify-email?token=`): panggil API sekali saat halaman dibuka (lindungi dari pemanggilan ganda saat render ulang). Berhasil → pesan sukses + tombol "Masuk" (atau "Ke dashboard" bila sudah login, sekaligus perbarui `session.user.email_verified`). Gagal (`400` field `token`) → "Tautan tidak valid atau sudah kedaluwarsa" + tombol kirim ulang (bila login) atau masuk.
- **Lupa password:** selalu tampilkan pesan yang sama setelah submit ("Jika email terdaftar, kami sudah mengirim tautan"), apa pun hasilnya kecuali `429`.
- **Reset password:** `password` + konfirmasi (dicek di klien). Berhasil → `/login` dengan toast "Password diganti, silakan masuk".
- **Konfirmasi ganti email:** seperti verifikasi email; `409` → "Email itu sudah dipakai akun lain".
- **Terima undangan** (`/accept-invitation?token=`):
  1. `GET /invitations/{token}`. `404` dengan `details.reason` → pesan sesuai alasan (`expired`, `revoked`, `accepted`, `declined`, `organization_deleted`); tanpa alasan → "Undangan tidak ditemukan".
  2. Tampilkan logo dan nama organisasi, pengundang, role, email, dan tanggal kedaluwarsa.
  3. Belum login: bila `account_exists` → tombol "Masuk untuk menerima" (`/login?redirect=/accept-invitation?token=...`); bila tidak → "Daftar untuk menerima" (`/register?invitation=...`). Tombol "Tolak" selalu ada (tidak butuh login).
  4. Sudah login: email sesi berbeda dengan email undangan → peringatan "Undangan ini untuk {email}" + tombol keluar. Email belum terverifikasi → pesan + tombol kirim ulang verifikasi. Selain itu tombol "Terima".
  5. Terima `200` → arahkan ke `/orgs/{id}` dari respons, toast "Anda bergabung ke {organisasi}". `403 invitation_email_mismatch` dan `403 EMAIL_NOT_VERIFIED` ditangani seperti poin 4.
  6. Tolak `204` → pesan "Undangan ditolak".
- **Logout:** `POST /auth/logout` (abaikan error), bersihkan state dan cache, kirim pesan `logout` lewat `BroadcastChannel('saka-auth')` supaya tab lain ikut keluar, arahkan ke `/login`.

### 6.5 Email belum terverifikasi

Bila `user.email_verified = false`, tampilkan banner warning di semua halaman aplikasi: "Verifikasi email Anda. Beberapa fitur (mengundang anggota, berlangganan) belum bisa dipakai." + tombol "Kirim ulang" (`POST /auth/resend-verification`; `429` → pesan batas; `409` → anggap sudah terverifikasi, refetch `me`). Error `403 EMAIL_NOT_VERIFIED` dari aksi mana pun memunculkan toast dengan pesan yang sama.

---

## 7. Penanganan Error dan Form

### 7.1 `ApiError`

Semua error API diubah menjadi:

```ts
class ApiError extends Error {
  status: number;             // HTTP status; 0 untuk gangguan jaringan
  code: ErrorCode | 'NETWORK_ERROR';
  serverMessage: string;      // error.message dari backend (Bahasa Inggris, bukan kontrak), hanya fallback
  details: Record<string, unknown>;
  retryAfterSeconds: number | null; // dari header Retry-After atau details.retry_after_seconds
  get reason(): string | null;      // details.reason bila ada
  get fieldErrors(): Record<string, string>; // details.fields untuk VALIDATION_ERROR
}
```

`errorMessage(err)` mengembalikan pesan **terjemahan frontend** berdasarkan `code` (dan `reason` bila ada), bukan `serverMessage`, karena pesan backend hanya berbahasa Inggris dan teksnya bukan kontrak yang stabil (spec backend §3), sementara UI mendukung id/en. `serverMessage` dipakai hanya bila kode tidak dikenal.

| Kode | Perlakuan default |
|---|---|
| `VALIDATION_ERROR` | Error per field di form (§7.2); field yang tidak ada di form (`body`, `cursor`) → pesan form umum |
| `UNAUTHENTICATED`, `TOKEN_EXPIRED` | Ditangani client (§6.3), tidak sampai ke komponen kecuali di halaman login |
| `FORBIDDEN` | `ErrorState` "Anda tidak punya akses" di halaman; toast untuk aksi. Beberapa `reason` punya pesan khusus (§7.3) |
| `EMAIL_NOT_VERIFIED` | Toast + banner (§6.5) |
| `PLAN_LIMIT_REACHED` | Dialog upgrade: "Plan {plan} hanya mendukung {max} {limit}" + tombol ke `/orgs/[orgId]/billing/plans` (sorot `details.upgrade_to`; tanpa tombol bila `null`) |
| `PLAN_FEATURE_UNAVAILABLE` | Dialog upgrade serupa dengan `details.feature` |
| `SUBSCRIPTION_INACTIVE` | Toast + tautan ke halaman billing |
| `NOT_FOUND` | `ErrorState` "Tidak ditemukan" di halaman; untuk `/orgs/[orgId]` → "Organisasi tidak ditemukan atau Anda bukan anggota" + tombol ke `/orgs` |
| `CONFLICT` | Pesan khusus per aksi atau per `reason` (§7.3) |
| `RATE_LIMITED` | "Terlalu banyak permintaan. Coba lagi dalam {n}" |
| `PAYMENT_PROVIDER_ERROR` | "Gateway pembayaran sedang bermasalah. Tidak ada yang ditagihkan, silakan coba lagi." |
| `INTERNAL_ERROR` | "Terjadi kesalahan pada server" + `X-Trace-ID` dari header respons (kecil, bisa disalin) untuk laporan |
| `NETWORK_ERROR` | "Tidak dapat terhubung ke server" + tombol coba lagi |

### 7.2 Form

Helper `createForm` di `lib/forms/form.svelte.ts`:

```ts
const form = createForm({
  schema: loginSchema,                 // Zod
  initial: { email: '', password: '' },
  onSubmit: async (values) => { ... }, // boleh melempar ApiError
});
// form.values (bindable $state), form.errors (Record<field, string>), form.formError (string | null),
// form.submitting (boolean), form.submit(event)
```

Perilaku:
1. Validasi Zod di klien sebelum request; pesan Zod memakai teks i18n.
2. `ApiError` `VALIDATION_ERROR` → `details.fields` dipetakan ke `form.errors` lewat `fieldErrorMessage(field, reason)` (`lib/forms/field-errors.ts`). Alasan backend berupa teks Inggris teknis (mis. `current_password: "is incorrect"`); pasangan yang dikenal diterjemahkan, sisanya memakai pesan umum "Nilai tidak valid".
3. Error lain → `form.formError = errorMessage(err)`, ditampilkan di atas tombol submit.
4. Tombol submit nonaktif dan menampilkan spinner selama `submitting`. Tidak ada submit ganda.
5. Fokus pindah ke field pertama yang error.

Pasangan `(field, reason)` yang wajib diterjemahkan:

| Field | Reason backend | Pesan (id) |
|---|---|---|
| `current_password` | `is incorrect` | Password saat ini salah |
| `new_password` | `must be different from the current password` | Password baru harus berbeda dari password saat ini |
| `new_email` | `must be different from the current email` | Email baru sama dengan email saat ini |
| `token` | `invalid or expired` | Tautan tidak valid atau sudah kedaluwarsa |
| `user_id` | `must be another member` | Pilih anggota lain |
| `plan` | `is not a known plan` | Plan tidak dikenal |
| (apa pun) | `is required` | Wajib diisi |

### 7.3 Pesan khusus per `reason`

| Konteks | `reason` | Pesan (id) |
|---|---|---|
| Keluar organisasi (`403`) | `owner_must_transfer_ownership` | Owner harus memindahkan kepemilikan sebelum keluar |
| Hapus akun (`409`) | `owns_shared_organization` | Anda masih owner organisasi yang punya anggota lain. Pindahkan kepemilikan atau hapus organisasi itu dulu |
| Terima undangan (`403`) | `invitation_email_mismatch` | Undangan ini untuk email lain |
| Ganti plan (`409`) | `use_checkout` | Langganan sudah berakhir; pilih plan untuk berlangganan lagi (frontend langsung memakai checkout, §8.2) |
| Checkout / ganti plan (`409`) | `already_subscribed` | Organisasi sudah berlangganan plan ini |
| Batal langganan (`409`) | `nothing_to_cancel` | Tidak ada langganan berbayar untuk dibatalkan |
| Lanjutkan langganan (`409`) | `nothing_to_resume` | Tidak ada langganan yang sedang dibatalkan |
| Undangan (`404`) | `expired` / `revoked` / `accepted` / `declined` / `organization_deleted` | Undangan sudah kedaluwarsa / dibatalkan / sudah diterima / sudah ditolak / organisasinya sudah dihapus |

---

## 8. Billing

### 8.1 Data

- `GET /plans` (publik): `code`, `name`, `price_amount` (rupiah, integer), `currency`, `interval`, `trial_days`, `features[]`, `limits{}` (`-1` = tak terbatas).
- `GET /orgs/{orgId}/subscription` (semua anggota): `plan`, `status`, `trial_ends_at`, `current_period_start`, `current_period_end` (`null` untuk Free), `cancel_at_period_end`, `pending_plan`, `grace_until`, `effective_plan`, `usage{limit: {used, max}}`.
- Nama fitur dan limit (`export_csv`, `audit_log`, `members`) diterjemahkan lewat i18n dengan kunci `billing_feature_<code>` dan `billing_limit_<code>`; kode yang belum punya terjemahan ditampilkan apa adanya.
- Uang diformat dengan `Intl.NumberFormat(locale, { style: 'currency', currency, maximumFractionDigits: 0 })`. Nilai dari API **tidak pernah** dibagi 100 (IDR tanpa desimal).

### 8.2 Tombol per plan: `resolvePlanAction(plan, subscription)`

Fungsi murni di `features/billing/plan-actions.ts`, dengan unit test untuk setiap baris tabel. `sub` = subscription organisasi; "berbayar" = `price_amount > 0`.

| Kondisi (dicek berurutan) | Aksi | Label |
|---|---|---|
| `sub.status` adalah `expired` atau `canceled` | plan berbayar → `checkout`; Free → `none` | "Berlangganan" / "Plan saat ini" |
| `plan.code === sub.plan` dan `sub.status` adalah `trialing` atau `past_due` | `change-plan` (plan yang sama; backend mengembalikan invoice yang bisa dibayar) | "Bayar sekarang" |
| `plan.code === sub.plan` dan `sub.pending_plan !== null` | `change-plan` (plan yang sama, membatalkan downgrade) | "Batalkan perubahan" |
| `plan.code === sub.plan` | `none` | "Plan saat ini" |
| `sub.plan === 'free'` (masih Free) | `checkout` | "Coba gratis {trial_days} hari" bila `trial_days > 0`, selain itu "Pilih plan" |
| `plan.code === sub.pending_plan` | `none` | "Terjadwal mulai {current_period_end}" |
| harga plan > harga plan sekarang | `change-plan` | "Upgrade" |
| selain itu | `change-plan` | "Downgrade" |

Catatan perilaku backend (`internal/billing/manage.go`): saat `trialing` atau `past_due`, memilih plan lain **tidak dijadwalkan** tetapi langsung membuat invoice plan itu; penjadwalan downgrade hanya terjadi saat `active`. Label "Downgrade" tetap dipakai, hasilnya dibaca dari respons (§8.3). Label "Coba gratis" hanya tebakan tampilan; backend yang memutuskan apakah trial masih tersedia (`trial_started` di respons). Hanya **owner** yang melihat tombol aksi; role lain melihat label "Hanya owner yang dapat mengubah plan".

### 8.3 Setelah checkout / change-plan

Respons `CheckoutResponse` atau `ChangePlanResponse`, diperiksa berurutan:

1. `trial_started === true` → toast "Trial {plan} dimulai, berakhir {trial_ends_at}", refetch subscription, kembali ke `/orgs/[orgId]/billing`.
2. `scheduled === true` → dialog info "Plan berganti ke {pending_plan} pada {current_period_end}". Bila `warnings` tidak kosong, tampilkan daftar "Pemakaian {limit} Anda ({used}) melebihi batas plan baru ({max}). Data tetap ada, tetapi Anda tidak bisa menambah yang baru."
3. `cleared === true` → toast "Perubahan plan dibatalkan".
4. Ada `invoice` dengan `payment_url` → simpan `{ orgId, invoiceId }` di `sessionStorage` key `saka.pendingPayment`, lalu `window.location.assign(payment_url)`.
5. Selain itu → refetch subscription.

Sebelum checkout pertama, tampilkan dialog konfirmasi berisi nama plan, harga per periode, dan catatan "Tidak ada proration: periode baru dimulai saat pembayaran diterima" (aturan backend §8.4).

### 8.4 Kembali dari pembayaran (`/billing/return`)

Status bayar **tidak pernah** diambil dari query string redirect gateway (aturan backend). Halaman ini:

1. Baca `saka.pendingPayment` dari `sessionStorage`. Tidak ada → arahkan ke `/` (aplikasi memilih organisasi).
2. Pastikan sesi (§6.2), lalu polling `GET /orgs/{orgId}/invoices/{invoiceId}` setiap 3 detik, maksimal 60 detik.
3. `status = paid` → hapus `saka.pendingPayment`, refetch subscription, tampilkan sukses + tombol ke billing.
4. Waktu habis → "Pembayaran sedang diproses. Status akan diperbarui otomatis dan kami mengirim email konfirmasi." + tombol ke billing. Jangan menampilkan "gagal".
5. `status` `expired`/`void` → "Invoice tidak berlaku lagi" + tombol ke halaman plan.

Pemilik repo mengatur **Finish/Unfinish/Error Redirect URL** Midtrans di dashboard Midtrans ke `<frontend_url>/billing/return` (tugas manual, dicatat di todo).

### 8.5 Banner status langganan

Fungsi murni `subscriptionBanner(sub, now)` di `features/billing/banners.ts` (unit test per baris) memilih satu banner untuk seluruh halaman organisasi:

| Kondisi (berurutan) | Varian | Pesan | Aksi (owner) |
|---|---|---|---|
| `status = past_due` | warning | Pembayaran tertunda. Akses {plan} berakhir {grace_until} | "Bayar sekarang" → daftar invoice |
| `status = trialing` dan `trial_ends_at` ≤ 7 hari lagi | info | Trial {plan} berakhir dalam {n} hari | "Bayar sekarang" → daftar invoice |
| `cancel_at_period_end = true` | warning | Langganan berhenti pada {current_period_end} | "Lanjutkan langganan" (resume) |
| `pending_plan !== null` | info | Plan berganti ke {pending_plan} pada {current_period_end} | "Batalkan perubahan" |
| `status` `expired`/`canceled` dan `sub.plan !== 'free'` | neutral | Langganan {plan} sudah berakhir; organisasi kembali ke Free | "Pilih plan" |
| selain itu | – | (tidak ada banner) | – |

Non-owner melihat banner yang sama tanpa tombol aksi.

### 8.6 Simulasi pembayaran di dev

Dengan `billing.provider = "fake"` (default dev backend), `payment_url` berbentuk `https://pay.fake.test/<nomor>` dan tidak bisa dibuka. Bila `import.meta.env.DEV` dan host `payment_url` adalah `pay.fake.test`, frontend **tidak redirect**, tetapi menampilkan dialog "Simulasi pembayaran (dev)" dengan tombol "Bayar" yang mengirim webhook bertanda tangan ke `POST /api/v1/webhooks/fake` (HMAC-SHA256 body dengan rahasia publik `fake-webhook-secret`, header `X-Fake-Signature`, format body sama dengan `docs/demo.md` Langkah 5), lalu membuka `/billing/return`. Kode ini ada di `src/lib/dev/fake-payment.ts`, hanya di-import di balik `if (import.meta.env.DEV)`, sehingga tidak ikut build production (diverifikasi di todo).

### 8.7 Halaman billing lain

- **Billing (`/orgs/[orgId]/billing`):** kartu plan efektif (`effective_plan` + `StatusBadge`), periode berjalan, tanggal trial/grace bila ada, pemakaian per limit (progress bar; `max = -1` → "Tak terbatas"; ≥ 80% → warna warning, 100% → destructive), tombol "Ubah plan" (ke halaman plan), "Batalkan langganan" (owner, bila plan berbayar dan `cancel_at_period_end = false`; konfirmasi "Akses tetap berlaku sampai {current_period_end}, tanpa refund"), "Lanjutkan langganan" (owner, bila `cancel_at_period_end = true`).
- **Plan (`/orgs/[orgId]/billing/plans`):** kartu per plan dari `GET /plans`: nama, harga per interval, daftar limit dan fitur, tombol dari §8.2. Plan sekarang ditandai.
- **Invoice (`/orgs/[orgId]/billing/invoices`):** tabel `number`, `plan_name`, `kind` (checkout/renewal, diterjemahkan), `amount`, `status`, `due_at`, `paid_at`; baris membuka detail. "Muat lebih banyak" (§3.3).
- **Detail invoice:** semua field, periode, dan tombol "Bayar" bila `payment_url` tidak `null` (perilaku sama dengan §8.3 poin 4 dan §8.6).

---

## 9. Fitur Organisasi, Anggota, dan Akun

### 9.1 Izin di UI: `can(role, action, target?)`

Fungsi murni di `features/org/permissions.ts`, menyalin matriks backend §7.2. Semua keputusan tampil/sembunyi tombol memakai fungsi ini (dilarang mengecek `role === 'owner'` langsung di komponen).

| `action` | owner | admin | member |
|---|:-:|:-:|:-:|
| `org.update` | ✓ | ✓ | – |
| `org.delete` | ✓ | – | – |
| `org.leave` | – | ✓ | ✓ |
| `ownership.transfer` | ✓ | – | – |
| `invitations.manage` | ✓ | ✓ | – |
| `member.changeRole` (target) | target bukan owner dan bukan diri sendiri | target `member` saja | – |
| `member.remove` (target) | target bukan owner dan bukan diri sendiri | target `member` saja | – |
| `billing.manage` | ✓ | – | – |
| `invoices.view` | ✓ | – | – |

`target` = `{ userId, role }` anggota yang akan diubah; fungsi juga menerima `selfUserId`. Role pemanggil diambil dari `GET /orgs/{orgId}` (`organization.role`), di-cache per organisasi, dan di-refetch setelah transfer kepemilikan.

### 9.2 Organisasi

- **Pemilih organisasi:** dropdown di atas sidebar, sumbernya `GET /me` (`organizations`: id, name, slug, role). Memilih → `/orgs/[id]`, simpan id di `localStorage` `saka.lastOrgId`. Item terakhir "Buat organisasi".
- **Buat (`/orgs/new`):** `name` (wajib, maks 200), `slug` (opsional; 3–48 karakter, huruf kecil/angka/tanda hubung tunggal; kosong = diturunkan backend dari nama). `409` → error di field `slug`. `429` → pesan batas (10 per jam). Berhasil → `/orgs/[id]`, invalidate `me`.
- **Pengaturan umum:** `name`, `slug`, `logo_url` (URL http/https; kosong → kirim `null` untuk menghapus). Hanya field yang berubah yang dikirim (`PATCH` menolak body kosong). Pratinjau logo dengan fallback inisial.
- **Transfer kepemilikan** (owner): pilih anggota (selain diri sendiri) + `current_password`. Berhasil (`204`) → invalidate organisasi, anggota, dan `me`; toast "{nama} sekarang owner".
- **Keluar** (admin, member): dialog konfirmasi → `POST .../leave` → hapus `saka.lastOrgId` bila sama, invalidate `me`, arahkan ke `/`.
- **Hapus** (owner): zona bahaya. Dialog meminta mengetik **nama organisasi** persis (pengaman di klien; backend tidak meminta password). Isi dialog menyebut akibatnya: anggota kehilangan akses, undangan dicabut, langganan dibatalkan, invoice terbuka dibatalkan. Berhasil → sama seperti keluar.

### 9.3 Anggota dan undangan

- **Anggota:** tabel avatar, nama, email, role, tanggal bergabung; "Muat lebih banyak". Aksi baris (menu titik tiga) sesuai `can()`: ubah role (`admin`/`member`), keluarkan (dialog konfirmasi). Diri sendiri ditandai "(Anda)".
- **Pemakaian:** di atas tabel tampilkan "{used} / {max} anggota" dari `subscription.usage.members` (anggota + undangan aktif; `-1` → tak terbatas).
- **Undang** (owner/admin): dialog `email` + `role` (`member` default, `admin`). `201` → toast "Undangan dikirim"; `200` → toast "Undangan diperbarui dan dikirim ulang"; `409` → "Email itu sudah menjadi anggota"; `402` → dialog upgrade (§7.1); `403 EMAIL_NOT_VERIFIED` → §6.5. Invalidate undangan dan subscription.
- **Undangan aktif:** tabel email, role, kedaluwarsa (relatif), aksi "Kirim ulang" (bisa `402`) dan "Batalkan" (konfirmasi).

### 9.4 Akun

- **Profil:** `name` (1–100), `avatar_url` (URL atau kosong → `null`; upload file belum ada di backend). Pratinjau avatar dengan fallback inisial. Email ditampilkan read-only dengan tautan ke "Ganti email".
- **Ganti password:** `current_password`, `new_password` (8–72), konfirmasi. Berhasil → toast "Password diganti. Sesi di perangkat lain sudah dikeluarkan."
- **Ganti email:** `new_email`, `current_password`. Berhasil → pesan "Kami mengirim tautan konfirmasi ke {email}. Email akun belum berubah sampai tautan dibuka." `409` → field `new_email` "Email sudah dipakai". `429` → batas 3 per jam.
- **Sesi:** daftar dari `GET /me/sessions`: perangkat (ringkasan `user_agent`: browser dan OS lewat fungsi kecil `describeUserAgent()` tanpa dependency, fallback teks asli dipotong), IP, mulai, terakhir dipakai, badge "Sesi ini". Tombol "Cabut" per sesi. Mencabut sesi ini = logout (§6.4).
- **Hapus akun:** penjelasan akibat (organisasi yang hanya berisi Anda ikut terhapus dan langganannya dibatalkan), centang "Saya mengerti", `current_password`. `409 owns_shared_organization` → pesan §7.3 + tautan ke daftar organisasi. Berhasil `204` → bersihkan sesi, `/login` dengan toast "Akun Anda sudah dihapus".

### 9.5 Dashboard ringkasan (`/orgs/[orgId]`)

Belum ada data bisnis (modul domain produk belum ada), jadi dashboard berisi kartu:

1. **Plan:** `effective_plan`, `StatusBadge`, tanggal penting (akhir periode/trial), tautan ke billing.
2. **Anggota:** pemakaian `members`, tombol "Undang" (bila `can('invitations.manage')`).
3. **Invoice belum dibayar** (owner saja): invoice `open` pertama dari `GET .../invoices` (halaman pertama) dengan tombol "Bayar"; tidak ada → kartu disembunyikan.
4. **Verifikasi email:** bila belum terverifikasi.
5. **Area modul domain:** `EmptyState` "Belum ada modul" yang menjadi tempat produk turunan menambah konten.

---

## 10. Internasionalisasi

- Bahasa: **`id` (dasar)** dan **`en`**. File: `web/messages/id.json`, `web/messages/en.json`.
- Strategi pemilihan locale Paraglide: `localStorage` → `preferredLanguage` (bahasa browser) → `baseLocale` (`id`). **Tanpa prefix URL** (aplikasi di balik login tidak butuh SEO per bahasa).
- Pemilih bahasa di menu user dan halaman publik; mengganti bahasa tidak me-reload data.
- **Kunci pesan:** `snake_case` berawalan area, mis. `auth_login_title`, `members_invite_button`, `billing_banner_past_due`, `error_rate_limited`, `common_save`. Parameter memakai sintaks Paraglide (`"Trial {plan} berakhir dalam {days} hari"`).
- Kedua file **harus punya kunci yang sama**; ada unit test yang gagal bila tidak (§11.1).
- Tanggal dan uang memakai `Intl` dengan locale aktif (`id-ID` / `en-US`) dan zona waktu browser. Waktu dari API berupa UTC (RFC 3339).
- Pesan dari backend (`message`, error maupun sukses) berbahasa Inggris dan bukan kontrak, jadi frontend selalu menampilkan teks i18n sendiri berdasarkan `code`/`reason` (§7.1); teks backend tidak pernah ditampilkan apa adanya kecuali sebagai fallback kode yang tidak dikenal. Email transaksional dikirim backend dalam Bahasa Inggris (spec backend §10.2).

---

## 11. Kualitas

### 11.1 Unit test (Vitest, wajib)

Fungsi murni dan logika inti di `lib/` dan `features/`:
- `lib/api`: `toApiError` (semua bentuk error, `Retry-After`, error jaringan), middleware Bearer.
- `lib/auth/refresh`: single-flight (dua pemanggil = satu request), retry sekali pada `TOKEN_EXPIRED`, sesi berakhir pada refresh gagal.
- `lib/forms`: pemetaan `details.fields`, pesan umum untuk reason tak dikenal.
- `lib/format`: uang IDR (`99000` → `Rp 99.000` di `id`), tanggal, relatif.
- `features/org/permissions`: setiap sel matriks §9.1.
- `features/billing/plan-actions` dan `banners`: setiap baris tabel §8.2 dan §8.5.
- `features/account/describeUserAgent`.
- i18n: `id.json` dan `en.json` punya kunci yang sama.

Komponen tidak wajib di-unit-test; perilakunya dicakup E2E.

### 11.2 E2E (Playwright, lokal)

Dijalankan terhadap backend asli (`make up migrate run run-worker`, `billing.provider = "fake"`, Mailpit). Email dibaca lewat API Mailpit (`http://localhost:8025/api/v1/...`) untuk mengambil tautan verifikasi/undangan/reset. Skenario minimum:
1. Registrasi → verifikasi lewat tautan email → login → dashboard.
2. Lupa password → reset → login dengan password baru.
3. Owner mengundang → penerima mendaftar dari tautan → verifikasi → menjadi anggota; owner mengubah role lalu mengeluarkannya.
4. Undang melebihi limit Free (3) → dialog upgrade.
5. Checkout Pro → trial dimulai → bayar invoice lewat simulasi fake (§8.6) → status `active`.
6. Ganti password, cabut sesi lain, logout.
7. Refresh token: dua tab aktif bersamaan setelah access token kedaluwarsa tidak menyebabkan logout (Web Locks).
8. Ganti bahasa ke Inggris dan tema ke dark tetap bertahan setelah reload.

E2E belum dijalankan di CI (butuh seluruh backend); target CI bisa ditambah kemudian.

### 11.3 Pemeriksaan wajib (lokal dan CI)

- `pnpm lint` (ESLint + Prettier check), `pnpm check` (`svelte-check`, nol error dan nol warning), `pnpm test` (Vitest), `pnpm build`.
- **Drift tipe API:** `pnpm api:generate` lalu `git diff --exit-code src/lib/api/schema.d.ts`. Bila `docs/openapi.yaml` berubah tanpa regenerate, CI gagal (pola yang sama dengan `sqlc`/`mockery` di backend).
- TypeScript `strict`; dilarang `any` eksplisit, `@ts-ignore`, dan `@ts-expect-error` tanpa komentar alasan.

### 11.4 Aksesibilitas

- Semua input punya `<label>`; error dihubungkan lewat `aria-describedby`; pesan form punya `role="alert"`.
- Semua aksi bisa dengan keyboard; focus ring selalu terlihat (`--ring`).
- Dialog konfirmasi destruktif memfokuskan tombol "Batal" lebih dulu.
- Ikon tanpa teks punya `aria-label` (terjemahan).
- Tidak ada informasi yang hanya disampaikan lewat warna (badge selalu berteks).

### 11.5 Keamanan

- Access token hanya di memori (§6.1). Tidak ada data sensitif di `localStorage` (hanya preferensi: tema, bahasa, `lastOrgId`).
- `redirect` setelah login hanya path internal (diawali `/`, bukan `//`, bukan `/\`).
- Tidak memakai `{@html}` untuk data dari API.
- URL avatar/logo dari user hanya dipakai di `<img src>` dengan skema `http(s)` (dicek sebelum render).
- Header keamanan (CSP, dll.) diatur di reverse proxy production; contoh konfigurasi ada di `web/docs/deploy.md` (dibuat di fase terakhir).

---

## 12. Kriteria Selesai

Frontend versi pertama selesai bila:

- [ ] Semua halaman §5 ada dan memakai endpoint yang tertulis, dengan keadaan memuat/error/kosong.
- [ ] Alur auth §6 berjalan, termasuk refresh single-flight dan antar tab.
- [ ] Semua aksi organisasi, anggota, undangan, akun, dan billing di §8–§9 bisa dilakukan dari UI sesuai role.
- [ ] Light/dark mode dan bahasa id/en lengkap, tidak ada teks yang tidak diterjemahkan.
- [ ] Unit test §11.1 dan skenario E2E §11.2 lulus; pemeriksaan §11.3 lulus di CI.
- [ ] Tidak ada warna mentah di komponen (`grep` kelas warna Tailwind bawaan kosong di `src/lib/components/app`, `src/lib/features`, `src/routes`).
- [ ] Build production tidak memuat kode simulasi pembayaran (`grep -r "fake-webhook-secret" build/` kosong).

## 13. Di Luar Cakupan Versi Pertama

Mengikuti backend Tier 1: upload avatar/logo (hanya URL), halaman audit log, export CSV, 2FA, login OAuth, panel super admin, notifikasi in-app, landing page marketing (disarankan proyek terpisah, mis. Astro). Dicatat di backlog [todo.md](todo.md).
