# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Status repo

Kode Go sudah ada. **Fase 0–5 selesai** (platform, outbox & mail, auth, user, organisasi/tim/role/RLS, entitlement); satu tugas Fase 4 menunggu tindakan manual: mendaftar akun sandbox Midtrans. **Fase 6 (billing: invoice, provider pembayaran, webhook, state machine, cron) berikutnya.** Acuan progres adalah centang di `docs/todo.md`, bukan ringkasan ini. Fase 6–7 belum ada; `internal/billing` baru berisi sisi baca (plan, subscription Free, plan efektif, `GET /plans`, `GET /orgs/{orgId}/subscription`) dan `internal/entitlement` lapisan kebijakan (`HasFeature`, `CheckLimit`, `Usage`, `RequireFeature`); Fase 6 menambah invoice, provider, dan state machine ke `billing`.

Perintah (semua lewat `Makefile`; `make help` menampilkan daftarnya):
- `make up` / `make down`: PostgreSQL 18, Mailpit, Redis via docker compose. `make config` membuat `config.json` dari `config.example.json`.
- `make migrate`, `migrate-down`, `migrate-status`, `migrate-new name=<snake_case>`: migrasi `goose` di `migrations/`.
- `make run` (API), `make run-worker` (worker outbox/email), `make build`.
- `make sqlc` dan `make mocks`: generate kode. **Hasilnya di-commit**; CI gagal bila ada drift (`make tidy mocks sqlc` lalu `git diff --exit-code`). Jalankan keduanya setelah mengubah `queries/*.sql`, `sqlc.yaml`, interface yang ada di `.mockery.yml`, atau menambah interface baru ke file itu.
- `make test` (unit, tanpa Docker), `make test-integration` (tag `integration`, butuh Docker untuk `testcontainers-go`), `make lint` (`golangci-lint` v2, dijalankan dengan dan tanpa tag `integration`).
- `make seed`: memeriksa plan default dan membuat akun demo terverifikasi (`demo@example.com`, password dicetak di output) beserta organisasinya, idempoten dan menolak `app.env=production` (`cmd/seed`, logika di `internal/seed`).

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
- **Cache member:** data member di jalur panas dibaca lewat `user.Service.GetSummary` (cache-aside, tanpa password hash). **Setiap method tulis di `user.Service` wajib berakhir dengan `invalidateAfterCommit`** (hook `db.AfterCommit`: cache dihapus setelah commit, bukan sebelum). Dengan begitu endpoint update/hapus member tidak perlu mengingat cache. Tidak ada endpoint HTTP untuk menghapus cache. Method tulis baru wajib punya test yang membuktikan cache terhapus: daftarkan namanya di `cacheWriters` (`internal/user/service_contract_test.go`, test reflection gagal bila ada method baru yang belum diklasifikasikan) dan di `writeCalls` (`cache_writers_integration_test.go`), yang menguji drop-after-commit, rollback, dan tanpa transaksi. Pengecualian (mis. `SetPasswordHash`: summary tidak memuat data password) harus ditulis di `notCacheWriters` beserta alasannya.
- Provider email produksi **ditunda ke Fase 7**; dev memakai Mailpit lewat interface `Mailer`.

## Arsitektur (butuh membaca beberapa bagian spec untuk paham)

Modular monolith: `cmd/api`, `cmd/worker`, dan `internal/{auth,user,org,billing,entitlement,mail,platform}`. Modul hanya boleh saling memanggil lewat interface/service publik, **bukan** mengakses tabel modul lain.

Urutan ketergantungan (juga urutan pengerjaan fase): `platform → mail/outbox → auth → user → org → entitlement → billing → hardening`. Jangan dibalik. Konsekuensinya: registrasi di Fase 2 hanya membuat user; pembuatan org ditambah di Fase 4 dan subscription `free` di Fase 5, sehingga acceptance criteria "daftar otomatis punya org dan plan Free" baru lengkap di akhir Fase 5.

### Peta kode dan kemudi dependensi

- **`internal/app`** adalah composition root (`NewRouter`): semua modul dibangun di sini lewat constructor, dan `cmd/api` serta test e2e memakai jalur yang sama. Tambah modul/route baru di sini. `app.Options` adalah tempat test menyuntikkan pengganti (mis. `Organizations`).
- Tiap modul punya `queries/*.sql` + paket `<modul>db` hasil `sqlc`; fungsi repository menerima `db.DBTX` (pool atau transaksi) supaya bisa dipakai dalam transaksi pemanggil.
- **Interface dideklarasikan oleh pemakainya, dan modul tidak boleh saling mengimpor melingkar.** `auth` mengimpor `user`, jadi `user` tidak boleh mengimpor `auth`. Contohnya: `user.Handler` melayani semua endpoint `/me`, tetapi hal yang dimiliki `auth` (password, token sekali pakai, sesi) dicapai lewat interface `user.AccountSecurity` yang diimplementasikan `auth.Service`; middleware autentikasi diteruskan ke `Routes(rg, authenticate)` dan handler membaca user/sesi dari `context.Context` (`reqctx`). Hal yang sama untuk hapus akun: `auth.OrganizationGuard` diimplementasikan modul org (Fase 4), sementara itu `auth.NoOrganizations`.
- **Tindakan sensitif butuh konfirmasi password lama** (ganti password, ganti email, hapus akun) lewat `auth.Service.confirmPassword`: password salah → `400` pada field `current_password` (bukan 401), tebakan gagal dibatasi 5/15 menit per user. Tindakan sensitif baru memakai helper yang sama. Keputusan rinci modul user ada di `docs/spec.md` §6.1.

### Aturan lintas modul yang mudah terlewat

- **Keanggotaan dari undangan hanya diberikan setelah email terbukti** (terima undangan butuh email terverifikasi; mendaftar dengan email yang diundang tidak membuat organisasi baru dan bergabung saat verifikasi). Jangan menggeser ke saat registrasi: siapa pun bisa mendaftar dengan email orang lain.

- **Isolasi tenant berlapis.** Route `/orgs/{orgId}/...` dipasang lewat `org.TenantRoutes` (autentikasi + tenant middleware): bukan anggota → **404** (bukan 403). `orgId` dan `role` hanya diambil dari `context.Context` (`reqctx.OrgID`, `org.RoleFrom`), tidak pernah dari input user; query tanpa `organization_id` dianggap bug. Otorisasi (matriks §7.2) dicek di service lewat `org.Authorize`. Lapisan kedua: **RLS aktif** (migrasi `00007`, detail dan alasannya di `docs/rls.md`): setiap koneksi aplikasi menjalankan `SET ROLE saka_app` (`database.app_role`), `db.WithTx` menyetel `app.current_org` dari context, dan kode org-scoped selalu berjalan di dalam transaksi (di luar transaksi RLS mengembalikan nol baris). Alur yang memang lintas tenant memakai `db.WithSystemTx` (atau `db.WithBypass` di tengah transaksi yang sudah ada); pakai seperlunya, `grep` mudah. Tabel baru dengan kolom `organization_id` **wajib** RLS + policy + index berawalan `organization_id` (test di `migrations/` gagal bila lupa). Test yang perlu melihat semua tenant memakai `dbtest.AdminPool` (pemilik tabel, tidak terkena RLS), bukan pool aplikasi. Penjaga rute: `TestTenantIsolation_*` mengambil daftar rute `/orgs/{orgId}` dari router; rute baru harus ditambahkan ke inventarisnya.
- **Role/org tidak ada di JWT.** JWT hanya `sub` dan `sid` (15 menit); role dicek dari DB per request agar perubahan langsung berlaku.
- **Trace ID & context propagation.** `trace_id`, `request_id`, `user_id`, `org_id` hidup di `context.Context` (key bertipe privat); semua fungsi I/O menerima `ctx` sebagai parameter pertama dan meneruskannya. Logger di-inject lewat constructor (wiring manual di `cmd/*`, tanpa logger global/framework DI) dan diambil dari context (`FromContext`) agar field ID otomatis ikut. `trace_id` disimpan di `outbox_jobs.trace_id` dan dipulihkan worker. Detail di spec §11.1.
- **Goroutine hanya lewat `platform/safego`.** Pekerjaan independen sebaiknya dijalankan paralel, tetapi `go func(){}()` mentah dilarang: helper `safego` menangkap panic (log + stack + `trace_id`), melacak goroutine untuk graceful shutdown, dan `Group` punya batas konkurensi. Setiap goroutine wajib berhenti saat `ctx` batal dan punya pemilik yang menunggunya. Jangan paralelkan operasi dalam satu transaksi (`pgx.Tx` tidak aman untuk akses bersamaan). Detail di spec §11.2.
- **Outbox untuk semua email.** Job dimasukkan ke `outbox_jobs` dalam **transaksi yang sama** dengan perubahan data; tidak ada pengiriman email sinkron di request. Worker memakai `FOR UPDATE SKIP LOCKED`, retry backoff 1m/5m/30m/2j/6j.
- **Idempoten untuk uang.** Webhook disimpan di `payment_events` dengan `UNIQUE(provider, event_id)`; verifikasi signature dulu (salah → 401); status bayar hanya dari webhook atau `GetPaymentStatus`, tidak pernah dari redirect klien. Semua job cron billing harus aman dijalankan ulang dan bersamaan.
- **Mesin langganan buatan sendiri** (gateway lokal tidak punya subscription berulang): invoice per periode → bayar → webhook → perpanjang. State machine di spec §8.2 harus berupa fungsi murni yang bisa diuji dengan fake `Clock`; karena itu semua kode waktu memakai interface `platform/clock`, bukan `time.Now()` langsung.
- **Entitlement:** pemakaian dihitung dari data aktual (`COUNT`), bukan counter terpisah; limit dicek di service layer dalam transaksi yang sama dengan pembuatan resource. Limit `members` = anggota + undangan aktif. Plan efektif = plan subscription bila statusnya memberi akses, selain itu `free`. Plan disimpan di DB lewat seed migration, jangan di-hardcode. Method `Entitlements` menerima `db.DBTX` (transaksi yang terpaku ke tenant, bukan pool); modul pemilik resource mendaftarkan counter-nya di composition root (`RegisterCounter`), dan kode yang menambah resource ber-limit mengunci baris organisasi lalu memanggil `CheckLimit` sebelum menulis (contoh: `org.Service.Invite`). Rute yang butuh fitur plan memakai `entitlements.RequireFeature(...)` di atas `org.TenantRoutes`. Keputusan rinci di `docs/spec.md` §9.4.
- **Konvensi data:** waktu `timestamptz` UTC; uang integer satuan terkecil (IDR tanpa desimal); token sekali pakai disimpan sebagai hash SHA-256; refresh token dirotasi dan pemakaian ulang token lama mencabut seluruh `family_id`; `forgot-password` selalu 200 (cegah user enumeration).
- Semua operasi multi-tabel dalam satu transaksi. Pagination cursor-based (`?limit=&cursor=`). Format error JSON `{error:{code,message,details}}` dengan kode di spec §3.

## Testing

Unit test service layer dengan mock hasil generate `mockery` untuk dependensi (repo, `Mailer`, `PaymentProvider`, dll.) dan fake `Clock` buatan sendiri (butuh state waktu yang bisa digeser, bukan sekadar mock); integration test dengan PostgreSQL asli (`testcontainers-go`) untuk repository, webhook, cron, dan **isolasi tenant** (user org A tidak boleh menyentuh data org B). Setiap acceptance criteria di spec §12 harus punya test otomatis.

Konvensi test yang sudah dipakai: file integration diberi `//go:build integration`; unit test service memakai `fixture_test.go` (mock + `clock.Fake`); e2e HTTP di `internal/app` memakai `newStack` (router asli + PostgreSQL + Redis + fake clock; lihat `stack_integration_test.go`). Tiap fitur ditulis bersama testnya, termasuk jalur gagal (rollback transaksi, rate limit, akses lintas user).

## Konvensi kode

- Semua kode ditulis dalam **bahasa Inggris**: nama variabel, fungsi, tipe, package, tabel/kolom DB, endpoint, kode error, komentar kode, dan pesan log. Dokumentasi di `docs/` tetap Bahasa Indonesia.
- Pesan error yang ditampilkan ke user (`message` di format error API) boleh mengikuti bahasa produk; `code`-nya tetap bahasa Inggris (mis. `PLAN_LIMIT_REACHED`).

## Git

Pesan commit memakai **Conventional Commits**: `<type>(<scope>): <description>`, dengan type seperti `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `build`, `ci`, `perf`. Scope opsional dan sebaiknya nama modul (mis. `feat(auth): add refresh token rotation`). Deskripsi dalam bahasa Inggris, huruf kecil, imperatif, tanpa titik di akhir. Perubahan breaking ditandai `!` atau footer `BREAKING CHANGE:`.

Remote `origin` memakai SSH (`git@github.com:kangsolder001/saka.git`). Repo ini mengonfigurasi `core.sshCommand` lokal ke key `~/.ssh/ssh-kgs` dan identitas commit `kangsolder001`; jangan memakai key `ssh-paxel` (akun kerja) di repo ini. Konfigurasi ini hanya ada di `.git/config`, bukan global.

Item di luar Tier 1 (spec §13: super admin, audit log, 2FA, OAuth, proration, kupon, dll.) jangan dikerjakan; catat ke backlog.
