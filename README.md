# Saka

Boilerplate SaaS multi-tenant yang bisa dipakai ulang, ditulis dengan Go dan PostgreSQL. Isinya fondasi yang selalu dibutuhkan produk SaaS: autentikasi, user, **organisasi** (tim, role, undangan), langganan dan penagihan, pembatasan fitur per plan, dan email transaksional. Begitu ada ide produk, Anda cukup menambahkan modul domain tanpa membangun ulang semua itu.

Prinsip utamanya: **organisasi adalah pusat**. Yang berlangganan adalah organisasi, bukan user, dan semua data bisnis milik sebuah organisasi. Isolasi antar organisasi dijaga berlapis (tenant middleware dan Row Level Security PostgreSQL).

Status: Tier 1 (spesifikasi v1.0). Pengecualian yang masih menunggu tindakan pemilik repo: verifikasi pembayaran di sandbox Midtrans sungguhan dan konfirmasi provider email produksi, lihat [docs/todo.md](docs/todo.md).

## Isi

- Auth: registrasi, login (JWT 15 menit + refresh token yang dirotasi), verifikasi email, lupa/reset password, batas laju.
- User: profil, ganti password dan email (dua langkah), sesi aktif, hapus akun.
- Organisasi: CRUD, anggota, role `owner`/`admin`/`member`, undangan, transfer kepemilikan.
- Billing: plan di database, langganan buatan sendiri (trial, perpanjangan, masa tenggang), invoice, webhook pembayaran idempoten (Midtrans, plus gateway palsu untuk dev/test), job terjadwal.
- Entitlement: limit dan fitur per plan (`PLAN_LIMIT_REACHED`, `PLAN_FEATURE_UNAVAILABLE`).
- Email: semua lewat outbox + worker, retry dengan backoff; driver Resend (produksi), SMTP (Mailpit di dev) dan log.

## Kebutuhan

- Go 1.26 atau lebih baru (disarankan patch terbaru: lihat catatan `govulncheck` di [spec §11.3](docs/spec.md))
- Docker dan Docker Compose (PostgreSQL 18, Redis, Mailpit untuk pengembangan; juga dipakai `testcontainers-go` pada integration test)
- `make`

Semua alat pembangkit kode (`goose`, `sqlc`, `mockery`) dipanggil lewat `go tool`, tidak perlu dipasang terpisah. `golangci-lint` diunduh otomatis ke `bin/` oleh `make lint`.

## Mulai cepat

```bash
make up           # PostgreSQL 18 (port 5433), Redis (6380), Mailpit (SMTP 1025, UI http://localhost:8025)
make config       # salin config.example.json menjadi config.json (hanya bila belum ada)
make migrate      # terapkan migrasi
make seed         # periksa plan default dan buat akun demo (demo@example.com; password dicetak)
make run          # API di http://localhost:8080
make run-worker   # worker: kirim email (outbox) dan job billing; jalankan di terminal lain
```

Periksa: `curl localhost:8080/healthz` dan `curl localhost:8080/readyz`. Email yang dikirim worker muncul di Mailpit (http://localhost:8025). Skenario lengkap dari daftar sampai langganan berakhir, dengan perintah `curl`, ada di [docs/demo.md](docs/demo.md).

`make help` menampilkan semua target. Beberapa yang sering dipakai:

| Target | Fungsi |
|---|---|
| `make migrate-status`, `migrate-down`, `migrate-new name=<snake_case>` | kelola migrasi `goose` di `migrations/` |
| `make sqlc` dan `make mocks` | bangkitkan kode; **hasilnya di-commit**, CI gagal bila ada selisih |
| `make test` | unit test (tanpa Docker) |
| `make test-integration` | unit + integration test (PostgreSQL dan Redis sungguhan lewat testcontainers) |
| `make lint` | `golangci-lint` dengan dan tanpa tag `integration` |
| `make loadtest ARGS="..."` | uji beban ringan terhadap API yang berjalan ([docs/loadtest.md](docs/loadtest.md)) |
| `make build` | biner `api`, `worker`, `migrate` ke `bin/` |

## Konfigurasi (`config.json`)

Konfigurasi **hanya dari berkas JSON**, bukan variabel lingkungan. Path lewat flag `-config` (default `./config.json`) pada `api`, `worker`, `migrate` dan `seed`. Kunci yang tidak dikenal ditolak saat start (salah ketik tidak diam-diam diabaikan), kunci yang tidak ditulis memakai default, dan semua masalah dilaporkan sekaligus dengan nama kuncinya. **`config.json` ada di `.gitignore`**; yang di-commit hanya `config.example.json` (berisi placeholder, dan harus selalu valid).

**Semua rahasia hanya di sini**: `auth.jwt_secret`, password di `database.url`, `redis.password`, `mail.smtp.password`, `mail.resend.api_key`, `billing.midtrans.server_key`. Nilainya tidak pernah ditampilkan di pesan error maupun log.

| Kunci | Default | Keterangan |
|---|---|---|
| `app.name` | `Saka` | nama produk di email |
| `app.env` | `development` | `development`, `test` atau `production`; production menerapkan validasi yang lebih ketat (lihat bawah) |
| `app.http_addr` | `:8080` | alamat dengar API |
| `app.shutdown_timeout` | `15s` | waktu tunggu graceful shutdown |
| `app.log_level` | `info` | `debug`, `info`, `warn`, `error` |
| `app.frontend_url` | `http://localhost:3000` | dasar tautan di email (`<url>/verify-email?token=...`) |
| `app.cors_allowed_origins` | `[]` | daftar origin browser yang boleh memanggil API; kosong = tanpa CORS; `*` ditolak |
| `app.trusted_proxies` | `[]` | IP/CIDR proxy yang `X-Forwarded-For`-nya dipercaya. Isi bila di belakang reverse proxy, kalau tidak batas laju per IP menghitung IP proxy |
| `database.url` | **wajib** | `postgres://user:password@host:port/db?sslmode=...` |
| `database.max_conns`, `min_conns` | `10`, `0` | ukuran pool |
| `database.app_role` | `saka_app` | role PostgreSQL non-owner yang dipakai tiap koneksi (`SET ROLE`) supaya RLS berlaku; wajib di production |
| `auth.jwt_secret` | **wajib** | minimal 32 karakter acak |
| `auth.access_token_ttl` | `15m` | harus lebih pendek dari refresh |
| `auth.refresh_token_ttl` | `720h` | |
| `redis.addr` | `localhost:6379` | satu Redis dipakai bersama (batas laju, cache); bukan sumber kebenaran |
| `redis.password`, `db` | kosong, `0` | |
| `redis.key_prefix` | `saka` | awalan semua key (`<prefix>:<modul>:`), supaya banyak environment bisa berbagi satu Redis |
| `redis.user_cache_ttl` | `5m` | batas kebasian cache member |
| `mail.driver` | `log` | `resend` (produksi), `smtp` (Mailpit/relay) atau `log` (dev saja; ditolak di production) |
| `mail.from` | `Saka <noreply@saka.local>` | pengirim; domainnya harus terverifikasi di provider |
| `mail.smtp.host`, `port`, `username`, `password` | `localhost`, `1025`, kosong | |
| `mail.resend.api_key` | kosong | wajib bila driver `resend`; setup domain di [docs/email.md](docs/email.md) |
| `mail.resend.base_url`, `timeout` | `https://api.resend.com`, `15s` | |
| `worker.concurrency` | `4` | job outbox paralel |
| `worker.poll_interval`, `job_timeout`, `lease` | `5s`, `1m`, `5m` | `lease` harus lebih panjang dari `job_timeout` |
| `billing.provider` | `fake` | `midtrans` atau `fake` (dev/test; ditolak di production) |
| `billing.invoice_due_days` | `7` | |
| `billing.renewal_lead_days` | `3` | invoice perpanjangan terbit H-3 |
| `billing.grace_days` | `3` | masa tenggang `past_due` |
| `billing.hourly_jobs_interval`, `daily_jobs_interval` | `1h`, `24h` | jadwal job di worker |
| `billing.reconcile_min_age` | `1h` | umur minimal invoice sebelum dicek ke gateway |
| `billing.midtrans.server_key` | kosong | wajib bila provider `midtrans` |
| `billing.midtrans.snap_base_url`, `api_base_url` | host sandbox | production wajib host produksi (`app.midtrans.com`, `api.midtrans.com`) |
| `billing.midtrans.request_timeout` | `15s` | |

Aturan `app.env = "production"`: `jwt_secret` tidak boleh berisi placeholder `change-me`, `database.app_role` wajib terisi, `mail.driver` bukan `log`, `billing.provider` bukan `fake`, kunci Midtrans bukan `SB-...` dan host bukan sandbox, URL Resend wajib `https`. Selain itu: ganti kredensial `saka:saka` pada `database.url` dengan milik Anda sendiri.

## Menjalankan di produksi (ringkas)

Dua proses dari konfigurasi yang sama: `bin/api` (HTTP) dan `bin/worker` (outbox email dan job billing; boleh lebih dari satu, dijaga `FOR UPDATE SKIP LOCKED` dan advisory lock). Jalankan `bin/migrate up` sebelum rilis. Pasang di belakang HTTPS (HSTS dikirim di production) dan isi `app.trusted_proxies`. Bangun dengan Go terbaru (kerentanan pustaka standar, spec §11.3). Daftar sisa pekerjaan yang butuh pemilik repo: [docs/todo.md](docs/todo.md).

## Tata letak proyek

```
cmd/
  api/ worker/ migrate/ seed/ loadtest/   biner: HTTP, outbox + job billing, migrasi, seed dev, uji beban
internal/
  app/            composition root (NewRouter): semua modul dirangkai di sini; test e2e memakai jalur yang sama
  auth/ user/ org/ billing/ entitlement/ mail/    modul domain (hanya saling memanggil lewat interface)
  platform/       config, db (WithTx, RLS), logger, httpx (middleware), apperr, ratelimit, redisx,
                  outbox, scheduler, safego (goroutine), clock, reqctx (trace_id/request_id/user_id/org_id)
  loadtest/ seed/ pustaka untuk cmd/loadtest dan cmd/seed
migrations/       migrasi SQL goose
templates/email/  template email (html dan teks)
docs/             dokumentasi (Bahasa Indonesia)
```

Tiap modul punya `queries/*.sql` dan paket `<modul>db` hasil `sqlc` (tanpa ORM). Interface dideklarasikan oleh pemakainya, modul tidak mengimpor satu sama lain secara melingkar. Gambaran lengkap, aturan lintas modul dan konvensi ada di [CLAUDE.md](CLAUDE.md).

## Dokumentasi

| Dokumen | Isi |
|---|---|
| [docs/spec.md](docs/spec.md) | spesifikasi Tier 1: model data, endpoint, aturan bisnis, keputusan implementasi, review keamanan (§11.3), audit observability (§11.4), kriteria selesai (§12) |
| [docs/openapi.yaml](docs/openapi.yaml) | referensi API (OpenAPI 3.1) untuk **semua** route, format error, autentikasi, paginasi; dijaga test agar tidak melenceng dari router |
| [docs/panduan-modul-baru.md](docs/panduan-modul-baru.md) | cara menambah modul domain baru (tenant, RLS, limit, fitur) |
| [docs/demo.md](docs/demo.md) | skenario demo end-to-end dengan `curl` |
| [docs/email.md](docs/email.md) | email produksi: Resend, SPF, DKIM, DMARC |
| [docs/loadtest.md](docs/loadtest.md) | uji beban ringan dan hasilnya |
| [docs/rls.md](docs/rls.md) | desain Row Level Security |
| [docs/plan.md](docs/plan.md), [docs/todo.md](docs/todo.md) | rencana fase dan progres |

## API sekilas

Basis `/api/v1`, JSON, `Authorization: Bearer <access_token>`. Resource organisasi di `/orgs/{orgId}/...` (bukan anggota = `404`). Error `{"error": {"code", "message", "details"}}` dengan kode stabil (`VALIDATION_ERROR`, `UNAUTHENTICATED`, `PLAN_LIMIT_REACHED`, `RATE_LIMITED`, ...). Daftar memakai cursor (`?limit=&cursor=`). Setiap respons membawa `X-Request-ID` dan `X-Trace-ID`. Detail per endpoint di [docs/openapi.yaml](docs/openapi.yaml).

## Pengujian

- `make test`: unit test, tanpa Docker (service dengan mock `mockery` dan `clock.Fake`).
- `make test-integration`: ditambah integration test di PostgreSQL dan Redis sungguhan (repository, webhook, job billing, isolasi tenant, e2e HTTP lewat router asli di `internal/app`, skenario demo penuh). Setiap kriteria di spec §12 punya test otomatis; `TestAcceptanceCriteria_EveryNamedTestExists` memastikan daftarnya tidak menunjuk test yang sudah hilang.
- Satu-satunya yang tidak terotomatisasi: verifikasi ke sandbox Midtrans dan Resend sungguhan. Test opsional `TestSandbox_*` (`internal/billing/payment/midtrans`) berjalan bila `config.json` Anda memakai provider `midtrans` dengan kunci sandbox.

## Kontribusi

Commit memakai Conventional Commits (`feat(auth): add ...`). Kode, komentar dan pesan log dalam bahasa Inggris; dokumen di `docs/` dan README dalam Bahasa Indonesia. Item di luar Tier 1 (spec §13) dicatat di backlog, bukan dikerjakan. Sebelum push: `make tidy mocks sqlc` tidak boleh menghasilkan selisih, `make lint` dan `make test-integration` lulus.
