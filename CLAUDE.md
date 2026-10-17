# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status repo

Saat ini repo **hanya berisi dokumentasi** di `docs/`; belum ada kode Go, `go.mod`, Makefile, atau CI. Belum ada perintah build/lint/test. Fase 0 di `docs/todo.md` yang akan membuatnya, termasuk Makefile (migrate, run, test, seed, sqlc). Jangan mengarang perintah sebelum itu ada.

Dokumen sumber, dibaca dalam urutan ini:
- `docs/spec.md`: spesifikasi Tier 1 (sumber kebenaran: model data, endpoint, aturan bisnis, acceptance criteria §12).
- `docs/plan.md`: urutan fase, keputusan yang sudah diambil, risiko.
- `docs/todo.md`: checklist tugas per fase dengan estimasi hari kerja. **Centang tugas yang selesai** dan perbarui bila lingkup berubah. Proyek dikerjakan di sela waktu luang, jadi checklist ini adalah penanda progres antar sesi.

Dokumen ditulis dalam Bahasa Indonesia; ikuti bahasa yang sama saat mengubahnya.

## Produk

Boilerplate SaaS multi-tenant (Go + PostgreSQL) yang bisa dipakai ulang: auth, user, organisasi/tim/role, billing, feature gating, email transaksional. **Organisasi** adalah pusat: yang berlangganan adalah organisasi, bukan user, dan semua data bisnis milik organisasi.

## Keputusan yang sudah final (jangan diulang tanyakan)

- Stack: `gin` (HTTP), `pgx` (driver), **`sqlc` untuk semua query, tanpa ORM/GORM**, `goose` (migrasi SQL), `zerolog` (logger), `testcontainers-go` (integration test), `mockery` (generate mock interface untuk unit test), **Redis** (`go-redis`) untuk state sementara.
- PostgreSQL **18**; UUID v7 lewat `uuidv7()` bawaan.
- Payment gateway pertama: **Midtrans**, di balik interface `PaymentProvider`.
- Registrasi masuk plan **Free**; trial dipicu saat memilih plan berbayar.
- **RLS PostgreSQL dipakai** sebagai lapisan isolasi tenant kedua.
- Nomor invoice **global**. Frontend belum ditentukan (API agnostik).
- Konfigurasi **hanya dari `config.json`** (path lewat flag `-config`, default `./config.json`), bukan env var. `config.json` ada di `.gitignore`; `config.example.json` di-commit dan harus selalu valid (ada test-nya). Setiap setting baru ditambahkan ke struct `platform/config`, ke `Default()`, dan ke `config.example.json`.
- **Redis satu instance, dipakai bersama oleh banyak modul** (rate limit sekarang; cache dan lainnya nanti). Pembagian peran: **PostgreSQL = sumber kebenaran** (user, org, langganan, invoice, uang, outbox); **Redis = state sementara/turunan** yang boleh hilang tanpa merusak data (rate limit, cache). Jangan menyimpan data yang harus tahan lama atau data uang hanya di Redis. Semua key diawali `<redis.key_prefix>:<modul>:` (mis. `saka:ratelimit:login:...`) lewat helper `platform/redisx`, supaya modul tidak saling menimpa dan satu Redis bisa dipakai banyak environment/service.
- **Cache member:** data member di jalur panas dibaca lewat `user.Service.GetSummary` (cache-aside, tanpa password hash). **Setiap method tulis di `user.Service` wajib berakhir dengan `invalidateAfterCommit`** (hook `db.AfterCommit`: cache dihapus setelah commit, bukan sebelum). Dengan begitu endpoint update/hapus member tidak perlu mengingat cache. Tidak ada endpoint HTTP untuk menghapus cache. Method tulis baru wajib punya test yang membuktikan cache terhapus.
- Provider email produksi **ditunda ke Fase 7**; dev memakai Mailpit lewat interface `Mailer`.

## Arsitektur (butuh membaca beberapa bagian spec untuk paham)

Modular monolith: `cmd/api`, `cmd/worker`, dan `internal/{auth,user,org,billing,entitlement,mail,platform}`. Modul hanya boleh saling memanggil lewat interface/service publik, **bukan** mengakses tabel modul lain.

Urutan ketergantungan (juga urutan pengerjaan fase): `platform → mail/outbox → auth → user → org → entitlement → billing → hardening`. Jangan dibalik. Konsekuensinya: registrasi di Fase 2 hanya membuat user; pembuatan org ditambah di Fase 4 dan subscription `free` di Fase 5, sehingga acceptance criteria "daftar otomatis punya org dan plan Free" baru lengkap di akhir Fase 5.

### Aturan lintas modul yang mudah terlewat

- **Isolasi tenant berlapis.** Route `/orgs/{orgId}/...` melewati tenant middleware: bukan anggota → **404** (bukan 403). `orgId` dan `role` hanya diambil dari `context.Context`, tidak pernah dari input user; query tanpa `organization_id` dianggap bug. Lapisan kedua: RLS dengan `SET LOCAL app.current_org` per transaksi (di helper `WithTx`), aplikasi konek dengan role DB **non-owner**, worker/migrasi butuh jalur bypass. Index tabel org-scoped diawali `organization_id`.
- **Role/org tidak ada di JWT.** JWT hanya `sub` dan `sid` (15 menit); role dicek dari DB per request agar perubahan langsung berlaku.
- **Trace ID & context propagation.** `trace_id`, `request_id`, `user_id`, `org_id` hidup di `context.Context` (key bertipe privat); semua fungsi I/O menerima `ctx` sebagai parameter pertama dan meneruskannya. Logger di-inject lewat constructor (wiring manual di `cmd/*`, tanpa logger global/framework DI) dan diambil dari context (`FromContext`) agar field ID otomatis ikut. `trace_id` disimpan di `outbox_jobs.trace_id` dan dipulihkan worker. Detail di spec §11.1.
- **Goroutine hanya lewat `platform/safego`.** Pekerjaan independen sebaiknya dijalankan paralel, tetapi `go func(){}()` mentah dilarang: helper `safego` menangkap panic (log + stack + `trace_id`), melacak goroutine untuk graceful shutdown, dan `Group` punya batas konkurensi. Setiap goroutine wajib berhenti saat `ctx` batal dan punya pemilik yang menunggunya. Jangan paralelkan operasi dalam satu transaksi (`pgx.Tx` tidak aman untuk akses bersamaan). Detail di spec §11.2.
- **Outbox untuk semua email.** Job dimasukkan ke `outbox_jobs` dalam **transaksi yang sama** dengan perubahan data; tidak ada pengiriman email sinkron di request. Worker memakai `FOR UPDATE SKIP LOCKED`, retry backoff 1m/5m/30m/2j/6j.
- **Idempoten untuk uang.** Webhook disimpan di `payment_events` dengan `UNIQUE(provider, event_id)`; verifikasi signature dulu (salah → 401); status bayar hanya dari webhook atau `GetPaymentStatus`, tidak pernah dari redirect klien. Semua job cron billing harus aman dijalankan ulang dan bersamaan.
- **Mesin langganan buatan sendiri** (gateway lokal tidak punya subscription berulang): invoice per periode → bayar → webhook → perpanjang. State machine di spec §8.2 harus berupa fungsi murni yang bisa diuji dengan fake `Clock`; karena itu semua kode waktu memakai interface `platform/clock`, bukan `time.Now()` langsung.
- **Entitlement:** pemakaian dihitung dari data aktual (`COUNT`), bukan counter terpisah; limit dicek di service layer dalam transaksi yang sama dengan pembuatan resource. Limit `members` = anggota + undangan aktif. Plan efektif = plan subscription bila statusnya memberi akses, selain itu `free`. Plan disimpan di DB lewat seed migration, jangan di-hardcode.
- **Konvensi data:** waktu `timestamptz` UTC; uang integer satuan terkecil (IDR tanpa desimal); token sekali pakai disimpan sebagai hash SHA-256; refresh token dirotasi dan pemakaian ulang token lama mencabut seluruh `family_id`; `forgot-password` selalu 200 (cegah user enumeration).
- Semua operasi multi-tabel dalam satu transaksi. Pagination cursor-based (`?limit=&cursor=`). Format error JSON `{error:{code,message,details}}` dengan kode di spec §3.

## Testing

Unit test service layer dengan mock hasil generate `mockery` untuk dependensi (repo, `Mailer`, `PaymentProvider`, dll.) dan fake `Clock` buatan sendiri (butuh state waktu yang bisa digeser, bukan sekadar mock); integration test dengan PostgreSQL asli (`testcontainers-go`) untuk repository, webhook, cron, dan **isolasi tenant** (user org A tidak boleh menyentuh data org B). Setiap acceptance criteria di spec §12 harus punya test otomatis.

## Konvensi kode

- Semua kode ditulis dalam **bahasa Inggris**: nama variabel, fungsi, tipe, package, tabel/kolom DB, endpoint, kode error, komentar kode, dan pesan log. Dokumentasi di `docs/` tetap Bahasa Indonesia.
- Pesan error yang ditampilkan ke user (`message` di format error API) boleh mengikuti bahasa produk; `code`-nya tetap bahasa Inggris (mis. `PLAN_LIMIT_REACHED`).

## Git

Pesan commit memakai **Conventional Commits**: `<type>(<scope>): <description>`, dengan type seperti `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `build`, `ci`, `perf`. Scope opsional dan sebaiknya nama modul (mis. `feat(auth): add refresh token rotation`). Deskripsi dalam bahasa Inggris, huruf kecil, imperatif, tanpa titik di akhir. Perubahan breaking ditandai `!` atau footer `BREAKING CHANGE:`.

Remote `origin` memakai SSH (`git@github.com:kangsolder001/saka.git`). Repo ini mengonfigurasi `core.sshCommand` lokal ke key `~/.ssh/ssh-kgs` dan identitas commit `kangsolder001`; jangan memakai key `ssh-paxel` (akun kerja) di repo ini. Konfigurasi ini hanya ada di `.git/config`, bukan global.

Item di luar Tier 1 (spec §13: super admin, audit log, 2FA, OAuth, proration, kupon, dll.) jangan dikerjakan; catat ke backlog.
