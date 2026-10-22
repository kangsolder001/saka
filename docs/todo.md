# TODO & Timeline — SaaS Boilerplate Tier 1

> Rujukan: [spec.md](spec.md) · [plan.md](plan.md)
> Asumsi: 1 backend developer dibantu AI agent, dikerjakan **di sela waktu luang** (tanpa deadline tanggal).
> Timeline berupa **durasi kerja efektif** (1 hari = 1 hari penuh fokus), bukan tanggal kalender.
> Format: `- [ ] tugas (estimasi hari)` · `§` = bagian di spec
> Estimasi sudah memperhitungkan bantuan AI (kira-kira 40% lebih cepat dari kerja manual).

## Ringkasan Durasi

| Fase | Nama | Durasi |
|---|---|---|
| 0 | Setup & Platform | 3 hari |
| 1 | Outbox & Mail | 3 hari |
| 2 | Auth | 6 hari |
| 3 | User | 3 hari |
| 4 | Organisasi, Tim & Role | 8 hari |
| 5 | Entitlement | 3 hari |
| 6 | Billing | 10 hari |
| 7 | Hardening & Acceptance | 4 hari |
| | **Total** | **40 hari kerja efektif** |
| | Buffer | 5 hari |

**Cara membaca:** jika Anda bisa menyisihkan ±2 jam/hari, 1 hari efektif ≈ 3–4 hari kalender. Total sekitar 4–6 bulan. Jika bisa 2 hari penuh tiap akhir pekan, total sekitar 20 minggu (±5 bulan).

Urutan fase tidak boleh dibalik. Lihat alasannya di [plan.md](plan.md). Tiap fase berakhir pada milestone yang bisa dijalankan dan diuji, jadi aman berhenti di antara fase.

---

## Fase 0 — Setup & Platform (3 hari)

- [x] Library diputuskan: `gin`, `pgx`, `sqlc`, `goose`, `zerolog`, `testcontainers-go`, `mockery`
- [x] Init repo Go, `go.mod`, struktur folder `cmd/` `internal/` `migrations/` `templates/email/` (§2) (0.25)
- [x] Konfigurasi `sqlc.yaml` dan `goose` (0.25)
- [x] Konfigurasi `mockery` (`.mockery.yml`, mock di-generate ke folder `mocks/` per package, target `make mocks`) (0.1)
- [x] `docker-compose`: PostgreSQL 18, Mailpit (0.25)
- [x] `platform/config`: load `config.json` (flag `-config`, contoh di `config.example.json`), default, validasi, tolak key tak dikenal (0.25)
- [x] `platform/db`: koneksi pool `pgx`, helper transaksi `WithTx` (0.25)
- [x] `platform/clock`: interface Clock + fake untuk test (0.1)
- [x] `platform/apperr` (nama package menghindari bentrok dengan `errors` bawaan): tipe error + format JSON & kode error (§3) (0.25)
- [x] `platform/logger`: `zerolog` JSON; logger di-inject lewat constructor, `FromContext(ctx)` menyertakan `trace_id`, `request_id`, `user_id`, `org_id` (§11.1) (0.25)
- [x] Context propagation: helper `platform` untuk simpan/ambil `trace_id`, `request_id`, `user_id`, `org_id` di `context.Context` (key bertipe privat) + unit test (0.15)
- [x] `platform/safego`: `Go(ctx, name, fn)` dengan `recover` + log (stack, `trace_id`) + pelacakan untuk shutdown, dan `Group` berbasis `errgroup` (panic → error, `SetLimit`) (§11.2) (0.25)
- [x] Test `safego`: panic tertangkap, error merambat, batas konkurensi, context batal, tidak ada goroutine tersisa (`goleak`) (0.15)
- [x] Middleware `gin`: request id + trace id (baca `traceparent`, set header `X-Request-ID`/`X-Trace-ID`), recover, logging, CORS, security headers (0.3)
- [x] Util: cursor pagination (`platform/pagination`) (0.1)
- [x] Util: validasi input + `BindJSON` (0.15). UUID v7 tidak butuh kode Go: dibuat oleh default kolom `uuidv7()` PostgreSQL 18, dipakai di migrasi
- [x] Endpoint `/healthz` dan `/readyz` (0.1)
- [x] Migrasi pertama: ekstensi `citext`, tabel `users` (§4.1) (0.1)
- [x] Setup CI (lint, unit test, integration test via `testcontainers-go`) (0.25). Workflow di `.github/workflows/ci.yml`; sudah hijau di GitHub (4 job: lint, unit, integration, drift generate)
- [x] Makefile / task runner: up/down, run, build, migrate*, sqlc, mocks, test, test-integration, lint (0.1). Target `seed` ditambahkan di Fase 5 bersama perintah seed

**Selesai bila:** API jalan, health check hijau, CI hijau.

---

## Fase 1 — Outbox & Mail (3 hari)

- [x] Migrasi `outbox_jobs` (§4.11, termasuk kolom `trace_id` dan `locked_at`) (0.1)
- [x] Repo outbox: `Enqueue(tx, job)` dalam transaksi pemanggil; simpan `trace_id` dari context ke `outbox_jobs.trace_id` (0.3)
- [x] Worker: polling dengan `SELECT … FOR UPDATE SKIP LOCKED`, status `processing/done/failed`; job diproses paralel (jumlah worker goroutine dikonfigurasi) lewat `safego` (0.5)
- [x] Retry exponential backoff 1m, 5m, 30m, 2j, 6j; retry ke-5 gagal → `failed` (total 6 percobaan, §10.1); error permanen langsung `failed` (0.3)
- [x] Interface `Mailer` + implementasi SMTP (Mailpit) + implementasi log (0.3)
- [x] Renderer template (`html/template` untuk HTML, `text/template` untuk plain text) + layout dasar (0.3)
- [x] Template awal: `verify_email`, `password_reset`, `password_changed` (0.3)
- [x] `cmd/worker` entrypoint + graceful shutdown; worker memulihkan `trace_id` job ke context sebelum proses (0.25)
- [x] Test: job diproses sekali saat 2 worker jalan; backoff sesuai jadwal (fake clock); rollback tx → tidak ada job; `trace_id` request sampai ke job dan log worker (0.6)

**Selesai bila:** email test terkirim ke Mailpit lewat outbox; AC Email (§12) untuk retry dan non-sinkron terpenuhi.

---

## Fase 2 — Auth (6 hari)

- [x] Config: section `auth` (jwt_secret, TTL) dan `app.frontend_url` (0.1)
- [x] Modul `user` minimal: tabel `users` via sqlc (create, get by id/email, tandai terverifikasi, ganti password) (0.3)
- [x] Migrasi `user_tokens`, `refresh_tokens` (§4.2, §4.3) (0.1)
- [x] Hashing Argon2id + validasi password (8–72, ≠ email) (0.25)
- [x] Util token acak + hash SHA-256; service `user_tokens` (buat, pakai sekali, expiry per tipe) (0.4)
- [x] `POST /auth/register` (user + job verifikasi; org & subscription di-stub, §5.2) (0.5)
- [x] `POST /auth/verify-email` + logika `ResendVerification` di service (0.2)
- [x] Route `POST /auth/resend-verification` (dipasang setelah middleware autentikasi, karena butuh user login) (0.1)
- [x] JWT: sign/verify, klaim `sub`/`sid`, 15 menit (§5.5) (0.3)
- [x] `POST /auth/login` (0.3)
- [x] Middleware autentikasi (JWT + user aktif + sesi/family masih valid, §5.6) + guard `EMAIL_NOT_VERIFIED` (0.3)
- [x] Refresh token: issue, rotasi, `replaced_by`, deteksi reuse → cabut family (§5.3) (0.7)
- [x] `POST /auth/refresh` (cookie HttpOnly untuk web, body untuk mobile) (0.3)
- [x] `POST /auth/logout` (0.1)
- [x] `POST /auth/forgot-password` (selalu 200) + `POST /auth/reset-password` (cabut semua refresh token, kirim `password_changed`) (0.5)
- [x] Rate limiter berbasis PostgreSQL (tabel `rate_limit_hits`, §5.6) + aturan per endpoint (§5.4) (0.7)
- [x] Test unit service auth; integration: rotasi, reuse detection, token sekali pakai, tidak ada user enumeration (0.7). Test ditulis bersama tiap unit; pemetaan AC ke test ada di spec §12

### Tambahan Fase 2: Redis (infrastruktur bersama)

- [x] `docker-compose`: Redis (host port 6380) + section `redis` di config (`addr`, `password`, `db`, `key_prefix`) (0.2)
- [x] `platform/redisx`: klien `go-redis`, helper key ber-namespace, ping + `/readyz` memeriksa Redis (0.3)
- [x] Pindahkan rate limiter ke Redis (sorted set + Lua atomik); hapus implementasi PostgreSQL + migrasi drop `rate_limit_hits`; test integrasi dengan Redis asli (0.6)
- [x] Cache data member di Redis (cache-aside, tanpa password hash, TTL pendek) dipakai middleware autentikasi, refresh, dan resend; invalidasi otomatis setelah commit lewat hook `db.AfterCommit` di setiap method tulis `user.Service` (tanpa endpoint HTTP) (0.7)

**Selesai bila:** AC Auth (§12) lulus, kecuali "otomatis punya organisasi dan plan Free" (menunggu Fase 4–5).

---

## Fase 3 — User (3 hari)

- [x] `GET /me` (daftar org diisi setelah Fase 4) dan `PATCH /me` (0.3). `organizations` sementara `[]`; endpoint dibaca dari `GetSummary` (cache), `PATCH` hanya menerima `name` dan `avatar_url` (null = hapus avatar)
- [x] `POST /me/password` (password lama wajib; cabut sesi lain kecuali saat ini; email notifikasi) (0.4). Plus batas 5 percobaan gagal / 15 menit per user (§6.1)
- [x] `POST /me/email` + `POST /me/email/confirm` + template `email_change_confirm`, `email_changed_notice` (0.6). Konfirmasi password + batas 3/jam per user; `confirm` tanpa login (§6.1)
- [x] `GET /me/sessions`, `DELETE /me/sessions/{id}` (0.3). ID sesi = `family_id` (= `sid`); sesi orang lain → 404 (§6.1)
- [x] `DELETE /me`: soft delete + anonimisasi email, cabut semua sesi; pengecekan owner lewat interface ke modul org (stub dulu) (0.4). Interface `auth.OrganizationGuard` (stub `auth.NoOrganizations`), butuh konfirmasi password (§6.1)
- [x] Test unit + integration modul user, termasuk: setiap method tulis baru (profil, email, hapus akun) memakai `invalidateAfterCommit` dan ada test yang membuktikan cache terhapus setelah commit dan tidak pernah basi (0.5). Test ditulis bersama tiap endpoint; yang tersisa: penjaga struktural `TestServiceMethods_AreAllClassified` + tabel `TestCacheWriters_*` (setelah commit, rollback, tanpa transaksi) yang gagal bila method tulis baru tidak terdaftar

**Selesai bila:** AC User (§12) lulus, kecuali penolakan hapus akun owner yang menunggu Fase 4.

---

## Fase 4 — Organisasi, Tim & Role (8 hari)

- [x] Keputusan #3: pakai RLS sebagai lapisan kedua (sekalian belajar)
- [x] Migrasi `organizations`, `memberships`, `invitations` + constraint unik (§4.4–4.6) (0.3). `migrations/00006`: slug tetap tercadangkan setelah soft delete; satu owner per org (partial unique index) dan `owner_id` wajib anggota org (FK komposit deferred); undangan terbuka unik per `(organization_id, email)` tanpa melihat kedaluwarsa, jadi mengundang ulang memperbarui baris lama (§7.4)
- [x] **Spike belajar RLS:** tabel dummy, policy, `SET LOCAL`, role DB non-owner, `EXPLAIN ANALYZE` dengan/tanpa RLS (1 juta baris) (1). Temuan dan desain di `docs/rls.md`; test `TestRLSSpike` menjaganya (jebakan utama: setting yang sudah berakhir terbaca `''`, bukan NULL, jadi policy wajib `NULLIF`)
- [x] Pembuatan org di registrasi: user + org + membership `owner` dalam 1 transaksi (§5.2) (0.3). `org.Service.CreateWithOwner` dipanggil lewat `auth.OrganizationProvisioner`; slug otomatis dari nama dengan akhiran acak bila bentrok
- [x] CRUD org: `GET/POST /orgs`, `GET/PATCH/DELETE /orgs/{orgId}` (slug unik) (0.6). Plus `GET /me` kini memuat daftar organisasi; keputusan di spec §7.6
- [x] Tenant context middleware (404 jika bukan anggota; orgId & role ke context) (§7.3) (0.4). `org.Tenant(...)`; role dicari dari DB tiap request, org yang dihapus sama dengan tak dikenal. Dikerjakan sebelum CRUD org supaya handler CRUD langsung memakainya
- [x] Authorization helper berdasarkan matriks izin (§7.2) (0.4). `org.Can`, `CanManageMember` (aturan target admin/owner), `Assignable`, `Authorize(ctx, action)` dan middleware `org.Require(action)`; test mentranskripsi matriks sel demi sel
- [x] RLS: role DB aplikasi non-owner, `SET LOCAL app.current_org` di `WithTx`, policy dengan `(SELECT current_setting(...))`, index berawalan `organization_id` (0.8). Migrasi `00007` (role `saka_app`, policy di `organizations`/`memberships`/`invitations`), `database.app_role` (`SET ROLE` per koneksi), `db.WithSystemTx` untuk alur lintas tenant, `db.PinTenant` untuk org yang baru dibuat. Index `(organization_id, ...)` sudah ada sejak `00006`
- [x] Test RLS: query tanpa filter org tidak bocor; lupa `SET LOCAL` → hasil kosong; worker/migrasi punya jalur bypass (0.3). `internal/org/rls_integration_test.go` + penjaga struktural di `migrations/` (setiap tabel ber-`organization_id` wajib RLS + policy dan punya index berawalan `organization_id`, jadi tabel Fase 5–6 otomatis terjaga)
- [x] Anggota: `GET members`, `PATCH role`, `DELETE member`, `POST leave` (0.6). Matriks izin diuji lewat e2e (`internal/app/members_integration_test.go`); keputusan di spec §7.6
- [x] Transfer kepemilikan atomik + konfirmasi password (§7.5) (0.4). Atomisitas diuji dengan trigger yang menggagalkan langkah terakhir, balapan dua transfer diuji dengan dua goroutine; `org.NewService` kini menerima `org.Dependencies`
- [x] Undangan: buat/list/batalkan/resend; perbarui undangan aktif yang sama (§7.4) (0.6). Limit `members` dipasang di Fase 5
- [x] Undangan: `GET /invitations/{token}`, accept (cek email sama), decline, kedaluwarsa 7 hari (0.6). Accept mewajibkan email terverifikasi; alasan di `details.reason` (spec §7.6)
- [x] Registrasi dengan email yang punya undangan → auto-accept, tanpa org baru (0.3). Tanpa org baru saat mendaftar; keanggotaan diberikan saat email terbukti (verifikasi / konfirmasi ganti email), bukan saat mendaftar, supaya tidak bisa dibajak dengan mendaftar memakai email korban (spec §7.6)
- [x] Template `invitation`; guard `EMAIL_NOT_VERIFIED` untuk undang (0.3). Template dikerjakan lebih awal (mail menolak template yang belum ada saat enqueue); guard dipasang pada `POST /invitations` dan `.../resend`
- [x] Hubungkan penolakan hapus akun owner ke modul org: implementasikan `auth.OrganizationGuard` (menolak bila owner org berisi anggota lain; hapus org yang hanya berisi dia + batalkan subscription) dan ganti `auth.NoOrganizations` di `internal/app` (0.3). Pembatalan subscription menyusul di Fase 6 (hook saat organisasi dihapus, baik lewat `DELETE /orgs/{orgId}` maupun hapus akun)
- [ ] **Daftar akun sandbox Midtrans** (0.1) ⚠️ lakukan di fase ini, persetujuan bisa lama. *Tindakan manual pemilik repo (akun dan Server Key tidak bisa dibuat dari kode); belum dikerjakan. Fase 6 ditulis terhadap dokumentasi Midtrans dan diuji dengan server palsu, jadi validasi sandbox sungguhan menunggu akun ini*
- [x] Test: matriks izin lengkap, isolasi tenant A vs B, transfer atomik, siklus undangan (0.7). Ditambah penjaga struktural: tes mengambil daftar rute `/orgs/{orgId}` dari router dan memastikan semuanya menjawab 404 untuk non-anggota, plus inventaris rute yang harus diperbarui bersama tes perannya

**Selesai bila:** AC Multi-tenancy & Tim (§12) lulus; AC User hapus akun lulus. ✅ (kecuali tugas pendaftaran akun sandbox di atas)

---

## Fase 5 — Entitlement (3 hari)

- [x] Migrasi `plans` + seed `free/pro/business` (§4.7, §9.3) (0.3). `migrations/00008`; plan global (bukan data tenant), tanpa RLS
- [x] Migrasi `subscriptions` (§4.8) (0.15). `00009`; `current_period_end` NULL untuk Free, RLS, backfill Free untuk org yang sudah ada
- [x] Registrasi membuat subscription `free` dalam transaksi yang sama (0.15). Juga `POST /orgs`; lewat `org.CreateWithOwner` → `billing.StartFree`
- [x] Plan efektif: status → akses fitur (§8.2, §9.2) (0.3). `billing.GrantsAccess` (murni) + `EffectivePlan`
- [x] Interface `Entitlements`: `HasFeature`, `CheckLimit`, `Usage` (hitung dari data aktual) (0.6). Counter terdaftar per limit; detail error di spec §9.4
- [x] Limit `members` = anggota + undangan aktif; pasang di alur undangan (§7.4) (0.3). `org.CountSeats`; `Invite` dan `resend` mengunci org lalu `CheckLimit`; test paralel
- [x] Middleware `RequireFeature(...)` + error `PLAN_LIMIT_REACHED` / `PLAN_FEATURE_UNAVAILABLE` berisi detail upgrade (0.3). Diuji lewat rute khusus test (`app.Options.TenantRoutes`)
- [x] `GET /plans`, `GET /orgs/{orgId}/subscription` (status + usage vs limit) (0.3). Handler di `internal/billing`; rute ditambahkan ke inventaris `TestTenantIsolation_*`
- [x] Perintah seed dev: plan default + akun demo (0.3). `make seed` → `cmd/seed` (`internal/seed`); plan berasal dari migrasi, seed memeriksanya
- [x] Test: limit terlampaui, org `expired` kembali ke limit Free (0.3). `internal/app/entitlement_integration_test.go`, `require_feature_integration_test.go`, `subscription_integration_test.go`; unit di `internal/entitlement`, `internal/billing`

**Selesai bila:** AC Entitlement + "daftar otomatis punya org & plan Free" lulus.

---

## Fase 6 — Billing (10 hari)

**Bagian A: pondasi & checkout**
- [ ] Spike Midtrans sandbox: buat payment, terima webhook, cek signature (0.5)
- [x] Migrasi `invoices`, `payment_events` (§4.9, §4.10) (0.3). `migrations/00010`: `invoices` ber-RLS (policy + index berawalan `organization_id`), kolom tambahan `kind` (`checkout`/`renewal`) dan penanda pengingat; indeks unik parsial mencegah dua checkout hidup atau dua invoice untuk periode renewal yang sama; `payment_events` global tanpa RLS; sequence `invoice_number_seq`
- [x] Interface `PaymentProvider` + implementasi Midtrans + fake provider untuk test (§8.1) (1). `internal/billing/payment` (+ `midtrans`, `fake`), config `billing`; Midtrans ditulis terhadap dokumentasi dan diuji dengan server palsu + vektor signature, **belum ke sandbox sungguhan** (asumsi yang perlu diverifikasi ada di spec §8.7)
- [x] Generator nomor invoice global `INV-YYYY-NNNNNN` (sequence) (0.15). `billing.NextInvoiceNumber`; sequence global yang tidak mulai ulang tiap tahun dan tidak ikut di-rollback (celah boleh, nomor ganda tidak)
- [ ] `POST …/subscription/checkout` (butuh role owner + email terverifikasi) → invoice + payment link (0.6)
- [ ] `GET …/invoices`, `GET …/invoices/{id}` (0.3)
- [ ] Template `invoice_created`, `payment_succeeded` (0.3)
- [ ] Test checkout end-to-end dengan fake provider (0.3)

**Bagian B: webhook & state machine**
- [ ] `POST /webhooks/{provider}`: verifikasi signature → 401 jika salah (§8.5) (0.4)
- [ ] Idempotensi `payment_events` `(provider, event_id)` (0.3)
- [ ] Proses webhook transaksional: invoice → subscription → job email; `processed_at`/`error`, 500 jika gagal (0.8)
- [x] State machine subscription (`trialing/active/past_due/expired/canceled`) sebagai fungsi murni + tabel transisi (§8.2) (0.6). `billing.Next`/`DueEvent`/`AddInterval`; tes menelusuri setiap pasangan status×event (13 transisi sah, sisanya `ErrIllegalTransition`) dengan `now` yang dipilih sendiri; aturan rinci di spec §8.7
- [ ] `change-plan` (upgrade via invoice; downgrade via `pending_plan_id`), `cancel`, `resume` (§8.4) (0.7)
- [ ] Test: webhook ganda diproses sekali, signature salah ditolak, semua transisi dengan fake clock (0.7)

**Bagian C: cron & notifikasi**
- [ ] Scheduler di worker + advisory lock (0.3)
- [ ] `billing.renewals` (invoice H-3) (0.3)
- [ ] `billing.expire` (active→past_due→expired, trial→expired, `grace_until` 3 hari) (0.5)
- [ ] `billing.apply_scheduled` (pending plan & cancel_at_period_end) (0.3)
- [ ] `billing.reminders` (H-3, H-1) + `billing.reconcile` (cek status `open` ke gateway) (0.8)
- [ ] Template: `payment_reminder`, `subscription_past_due`, `subscription_expired`, `trial_ending` (0.4)
- [ ] Test cron idempoten & aman bersamaan (jalan 2×, 2 worker) (0.5)
- [ ] Test reconcile memperbaiki invoice dengan webhook hilang (0.2)

**Selesai bila:** AC Billing (§12) lulus di sandbox Midtrans.
*Jika terasa terlalu besar: geser `reminders` dan `trial_ending` ke Fase 7.*

---

## Fase 7 — Hardening & Acceptance (4 hari)

- [ ] Telusuri seluruh checklist §12 ↔ test otomatis; tutup celah (0.6)
- [ ] Review keamanan: header, CORS, validasi input, secret hanya di `config.json` (tidak di git), query parametrized, rate limit (0.6)
- [ ] Observability: audit bahwa `user_id` & `org_id` & `trace_id` muncul di log lintas modul, log job gagal, token/password tidak bocor ke log (0.3)
- [ ] Uji beban ringan endpoint auth & webhook (0.3)
- [ ] Putuskan provider email produksi (Resend/Brevo/SES) + implementasi `Mailer` + setup SPF/DKIM domain (0.5)
- [ ] Dokumentasi: README (setup, `config.json`, jalankan API + worker), daftar endpoint/OpenAPI (0.6)
- [ ] Panduan "cara menambah modul domain baru" (tenant context + CheckLimit) (0.3)
- [ ] Demo end-to-end skenario penuh (daftar → undang → upgrade → expired) (0.3)
- [ ] Update spec ke v1.0 + catat backlog Tier 2 (0.2)

**Selesai bila:** semua checklist §12 tercentang, Tier 1 selesai.

---

## Buffer (5 hari)

Dipakai untuk bug, review, atau fase yang molor. Fase yang paling rawan molor: Fase 6 (gateway eksternal) dan Fase 4 (RLS, karena sekalian belajar).

## Backlog (di luar Tier 1, §13 spec)

Panel super admin · audit log · notifikasi in-app · 2FA · OAuth · upload file · proration · kupon · pajak · multi-currency · API publik
