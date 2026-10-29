# Saka Web Frontend

Frontend antarmuka web modern untuk SaaS multi-tenant Saka. Dibangun sebagai **Single Page Application (SPA) statis** berbasis Svelte 5, SvelteKit, dan Tailwind CSS v4 dengan arsitektur enterprise: autentikasi sesi JWT aman di memori, manajemen organisasi multi-tenant, tagihan & langganan Midtrans, internasionalisasi (i18n), dan tema terang/gelap.

---

## 🛠 Teknologi Utama

- **Framework:** [Svelte 5](https://svelte.dev/) (Runes `$state`, `$derived`, `$props`) + [SvelteKit](https://kit.svelte.dev/) dengan `@sveltejs/adapter-static` (SPA fallback `index.html`).
- **Styling:** [Tailwind CSS v4](https://tailwindcss.com/) dengan Semantic Design Tokens (WCAG AA) dan tema Light/Dark via `mode-watcher`.
- **Komponen UI:** [shadcn-svelte](https://shadcn-svelte.com/) / [Bits UI](https://bits-ui.com/) + ikon vektor profesional [Lucide Svelte](https://lucide.dev/).
- **Data Fetching & State:** [TanStack Query v5](https://tanstack.com/query/latest) (`@tanstack/svelte-query`) untuk query caching, invalidation, dan infinite pagination.
- **API Client:** `openapi-fetch` tipe-aman yang dibangkitkan dari spesifikasi OpenAPI backend (`docs/openapi.yaml`).
- **Internasionalisasi (i18n):** [Paraglide JS](https://inlang.com/m/gerre34r/library-inlang-paraglideJs) dengan kompilasi waktu build tanpa overhead runtime (Bahasa Indonesia `id` & English `en`).
- **Testing:**
  - **Unit & Browser Component Tests:** [Vitest](https://vitest.dev/) + `@vitest/browser` (Chromium).
  - **End-to-End (E2E):** [Playwright](https://playwright.dev/) terintegrasi dengan Mailpit API untuk verifikasi email otomatis.

---

## 🚀 Memulai Pengembangan

### Prasyarat

- Node.js 22 LTS atau lebih baru
- `pnpm` v10 atau lebih baru
- Backend Saka berjalan di `http://localhost:8080` (lihat root `README.md`)
- Mailpit berjalan di `http://localhost:8025`

### Instalasi Dependensi

```bash
pnpm install
```

### Menjalankan Development Server

```bash
pnpm dev
```

Aplikasi dapat diakses di `http://localhost:3000`. Permintaan `/api/*` secara otomatis di-proxy ke `http://localhost:8080`.

---

## 📋 Daftar Perintah (Scripts)

| Perintah            | Deskripsi                                                          |
| ------------------- | ------------------------------------------------------------------ |
| `pnpm dev`          | Menjalankan Vite dev server pada port 3000 dengan API proxy aktif. |
| `pnpm build`        | Membangun SPA statis produksi ke folder `web/build/`.              |
| `pnpm preview`      | Menjalankan preview lokal dari hasil build `web/build/`.           |
| `pnpm check`        | Memeriksa validasi tipe TypeScript & Svelte (`svelte-check`).      |
| `pnpm lint`         | Menjalankan pemeriksaan Prettier dan ESLint.                       |
| `pnpm format`       | Menjalankan pemformatan otomatis kode dengan Prettier.             |
| `pnpm test`         | Menjalankan seluruh pengujian unit & browser test dengan Vitest.   |
| `pnpm test:unit`    | Menjalankan unit test saja di lingkungan server/Node.js.           |
| `pnpm test:browser` | Menjalankan browser component test menggunakan Chromium headless.  |
| `pnpm test:e2e`     | Menjalankan seluruh skenario Playwright E2E terhadap backend asli. |
| `pnpm api:generate` | Memperbarui berkas tipe `schema.d.ts` dari `../docs/openapi.yaml`. |

---

## 📂 Struktur Direktori

```
web/
├── docs/
│   ├── spec.md         # Spesifikasi teknis frontend v1.0
│   ├── deploy.md       # Panduan deployment produksi (Nginx/Caddy, CSP, SSL)
│   └── todo.md         # Catatan progres pekerjaan frontend
├── messages/           # Kamus terjemahan i18n
│   ├── id.json         # Bahasa Indonesia (default base)
│   └── en.json         # English
├── src/
│   ├── lib/
│   │   ├── api/        # Client openapi-fetch, query keys, query client
│   │   ├── auth/       # State sesi in-memory, token refresh single-flight, Web Locks
│   │   ├── components/ # Komponen global (AppShell, UserMenu, StatusBadge, Banner, dsb.)
│   │   │   └── ui/     # Primitif shadcn-svelte/Bits UI
│   │   ├── dev/        # Helper simulasi pembayaran dev (Fake Payment HMAC-SHA256)
│   │   ├── features/   # Modul fitur: org, members, invitations, billing, account
│   │   ├── format/     # Formatter tanggal & mata uang berbasis Intl
│   │   └── paraglide/  # Artefak hasil kompilasi runtime i18n Paraglide
│   └── routes/         # Struktur halaman SvelteKit (SPA)
│       ├── (app)/      # Halaman yang memerlukan autentikasi (App shell, dashboard, billing, dll.)
│       └── (public)/   # Halaman publik (Login, Register, Reset Password, Return Payment, dll.)
└── tests/
    └── e2e/            # Pengujian Playwright E2E (skenario 1-8 lengkap)
```

---

## 🚢 Panduan Deployment

Panduan lengkap mengenai arsitektur Same-Origin, konfigurasi Nginx / Caddy, Content-Security-Policy (CSP), dan integrasi callback pembayaran Midtrans tersedia di **[docs/deploy.md](docs/deploy.md)**.
