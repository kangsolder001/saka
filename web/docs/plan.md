# Plan Implementasi — Frontend Web

> Sumber: [spec.md](spec.md) · Checklist: [todo.md](todo.md) · Aturan kerja agent: [../CLAUDE.md](../CLAUDE.md)
> Asumsi: dikerjakan **di sela waktu luang** oleh 1 developer dibantu AI agent, termasuk model yang lebih murah. Durasi berupa hari kerja efektif (1 hari = 1 hari penuh fokus), seperti plan backend.

---

## 1. Strategi

Bangun dari fondasi ke fitur, mengikuti ketergantungan:

```
W0 setup ─▶ W1 fondasi API & sesi ─▶ W2 app shell ─▶ W3 auth publik ─▶ W4 akun ─▶ W5 organisasi & anggota ─▶ W6 billing ─▶ W7 penyelesaian
```

Alasan urutan:
1. **Fondasi dulu (W0–W2).** API client, refresh token, form helper, i18n, token warna, dan komponen bersama dipakai semua halaman. Bila fondasi benar, halaman fitur tinggal merangkai pola yang sama, dan model yang lebih murah cukup meniru contoh.
2. **Auth publik sebelum fitur (W3).** Semua halaman aplikasi butuh login, dan e2e butuh registrasi/verifikasi.
3. **Akun sebelum organisasi (W4).** Halaman akun sederhana (satu user, tanpa role), cocok untuk mematangkan pola form dan mutation sebelum halaman ber-role.
4. **Billing paling akhir (W6)** karena logikanya paling banyak (tombol plan, banner, redirect gateway, polling) dan bergantung pada halaman organisasi.

## 2. Fase dan Milestone

| Fase | Nama | Durasi | Milestone (definisi selesai) |
|---|---|---|---|
| W0 | Setup proyek | 2 hari | `pnpm dev` jalan di port 3000 dengan proxy ke API, token tema light/dark, i18n id/en, lint/check/test/build hijau, target `make web-*`, CI frontend |
| W1 | Fondasi API & sesi | 3 hari | Tipe dari OpenAPI, client dengan Bearer + refresh single-flight + Web Locks, `ApiError`, form helper, formatter; semuanya ber-unit-test |
| W2 | App shell & komponen bersama | 2 hari | Layout publik dan aplikasi responsif (sidebar desktop + drawer mobile), pemilih organisasi, menu user, tema, bahasa, banner, `StatusBadge`, `EmptyState`, `ErrorState`, `ConfirmDialog`, `LoadMore` |
| W3 | Auth publik & tautan email | 2,5 hari | Login, registrasi (biasa dan dari undangan), lupa/reset password, verifikasi email, konfirmasi ganti email, terima undangan, logout |
| W4 | Akun | 1,5 hari | Profil, ganti password, ganti email, sesi, hapus akun |
| W5 | Organisasi, anggota & undangan | 3 hari | Buat/pilih organisasi, pengaturan, transfer, keluar, hapus, anggota, undangan, dialog upgrade, dashboard ringkasan |
| W6 | Billing | 3 hari | Halaman billing, plan, checkout, ganti plan, batal/lanjutkan, invoice, return page, banner, simulasi bayar dev |
| W7 | Penyelesaian & acceptance | 2 hari | E2E §11.2 lulus, audit aksesibilitas, teks, dan responsivitas (mobile 320px & desktop), panduan deploy, kriteria §12 spec tercentang |
| – | Buffer | 2 hari | Cadangan |
| | **Total** | **19 hari efektif + 2 buffer** | |

Dengan ±2 jam/hari, 1 hari efektif ≈ 3–4 hari kalender (total sekitar 2–2,5 bulan). Tiap fase berakhir pada aplikasi yang bisa dijalankan, jadi aman berhenti di antara fase.

## 3. Keputusan

| # | Keputusan | Pilihan | Alasan |
|---|---|---|---|
| 1 | Framework | **SvelteKit + Svelte 5**, mode **SPA statis** | Ringan, rapi; auth sudah di Go sehingga tidak perlu server Node |
| 2 | Lokasi kode | `web/` di repo yang sama (monorepo) | Perubahan OpenAPI dan frontend bisa satu commit; CI menjaga keduanya cocok |
| 3 | Dokumentasi | `web/docs/` (terpisah dari `docs/` backend) | Dokumen frontend dan backend tidak tercampur |
| 4 | Tema | **Putih & Biru** (light) serta **Hitam & Biru Dark** (dark), light/dark/system | Estetika enterprise bersih & tegas; menghindari tampilan klise AI generator (spec §4) |
| 5 | Bahasa | **Indonesia (dasar) + Inggris**, Paraglide, tanpa prefix URL | Aplikasi di balik login; ganti bahasa tanpa reload |
| 6 | Komponen | shadcn-svelte (disalin ke repo) | Bebas diubah, bukan dependency tertutup |
| 7 | Data | TanStack Query (runes) | Cache, loading/error, invalidasi tanpa store buatan sendiri |
| 8 | API client | openapi-typescript + openapi-fetch, tipe di-commit + cek drift CI | Kontrak dijaga compiler, pola sama dengan `sqlc` |
| 9 | Form | Zod + helper kecil sendiri, **bukan** superforms | Superforms dirancang untuk form action di server; di SPA helper ~100 baris lebih mudah dipahami agent murah |
| 10 | Token | Access token di memori, refresh lewat cookie `HttpOnly` | Mengikuti kontrak backend (spec backend §5.6) |
| 11 | Deploy | Same-origin (reverse proxy `/api`) disarankan; subdomain didukung | Cookie `SameSite=Strict` |
| 12 | E2E | Playwright terhadap backend lokal asli, belum di CI | Butuh seluruh backend; CI menyusul |
| 13 | Icon | **Icon vektor SVG** (`lucide-svelte` / Flaticon SVG / Tabler), **dilarang emotikon/emoji** | Tampilan profesional; emoji membuat aplikasi terkesan murahan dan AI template |
| 14 | Responsif | **Mobile-first & Desktop fluid** (viewport 320px – 1440px+), drawer Sheet di mobile | Pengalaman nyaman di ponsel dan layar monitor desktop tanpa horizontal overflow (spec §4.4) |

## 4. Risiko

| Risiko | Dampak | Mitigasi |
|---|---|---|
| Refresh paralel memicu deteksi pencurian token → user ter-logout | Tinggi | Single-flight + Web Locks (spec §6.3), unit test + skenario E2E khusus |
| Versi baru (SvelteKit 3, Svelte 5, Tailwind 4, Zod 4, Paraglide 2) belum dikenal baik oleh model AI | Sedang | `web/CLAUDE.md` mewajibkan membaca dokumentasi resmi (`llms.txt`) sebelum menebak API; pola dibuat sekali di W1–W2 lalu ditiru |
| Model murah menulis ulang pola dengan cara berbeda | Sedang | Tugas di todo menyebut file, nama fungsi, dan contoh yang harus ditiru; satu halaman referensi (`/account`) jadi pola halaman form |
| Teks yang lupa diterjemahkan | Rendah | Test kesamaan kunci id/en; aturan "tidak ada teks hardcode" diperiksa saat review |
| Midtrans sandbox belum pernah diuji (backend) | Sedang | Dev memakai provider `fake` dengan simulasi bayar; redirect URL Midtrans dicatat sebagai tugas manual pemilik repo |
| Backend mewajibkan `organization_name` walau registrasi dari undangan | Rendah | Frontend mengisi otomatis (spec §6.4); usulan perubahan backend dicatat di todo |
| Teks `message` backend berubah bahasa (Indonesia → Inggris, Fase 8 backend) | Rendah | Frontend tidak pernah menampilkan `message` kecuali fallback; semua teks dari i18n berdasarkan `code`/`reason` (spec §7.1) |

## 5. Cara kerja per tugas (untuk agent)

1. Baca tugas di [todo.md](todo.md), bagian spec yang dirujuk, dan file contoh yang disebut.
2. Kerjakan **satu tugas** saja, termasuk test-nya.
3. Jalankan `make web-check` (lint, check, test, build). Semua harus hijau.
4. Centang tugas di todo. Bila lingkup berubah atau ada temuan, tulis di todo (bagian catatan fase atau "Catatan untuk backend").
5. Commit dengan Conventional Commits scope `web`, mis. `feat(web): add login page`.
