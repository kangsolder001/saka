# SaaS Boilerplate — Spesifikasi Tier 1

> Status: Draft v0.1
> Cakupan: fondasi inti (Auth, User, Multi-tenancy, Tim & Role, Billing, Feature Gating, Email Transaksional)

---

## 1. Tujuan

Membangun fondasi SaaS yang bisa dipakai ulang untuk produk apa pun. Begitu ada ide produk, developer cukup menambahkan modul domain tanpa membangun ulang login, organisasi, tim, dan langganan.

### Prinsip desain

1. **Organisasi adalah pusat.** Yang berlangganan adalah organisasi, bukan user. Semua data bisnis milik sebuah organisasi.
2. **Aman secara default.** Query tanpa `organization_id` dianggap bug.
3. **Sederhana dulu.** Pilih solusi paling sederhana yang benar. Fleksibilitas ditambah saat ada kebutuhan nyata.
4. **Idempoten untuk hal yang menyangkut uang.** Webhook dan job boleh diproses berkali-kali tanpa efek ganda.
5. **Modular monolith.** Satu aplikasi, modul terpisah dengan batas yang jelas. Tidak perlu microservice di tahap ini.

---

## 2. Stack & Konvensi

| Aspek | Pilihan |
|---|---|
| Bahasa | Go |
| Database | PostgreSQL 18+ |
| Migrasi | File SQL terversi (mis. `golang-migrate` atau `goose`) |
| ID | UUID v7 (urut waktu, aman diekspos) |
| Waktu | Disimpan dalam UTC (`timestamptz`) |
| Uang | Integer dalam satuan terkecil (IDR → rupiah, tanpa desimal) |
| Password hashing | Argon2id |
| Access token | JWT, masa berlaku 15 menit |
| Refresh token | Token acak opaque, disimpan dalam bentuk hash, masa berlaku 30 hari, dirotasi |
| Background job | Outbox table + worker (queue eksternal seperti RabbitMQ opsional) |
| Payment gateway | Diabstraksi lewat interface; implementasi pertama Midtrans atau Xendit |

### Struktur proyek

```
cmd/
  api/            # HTTP server
  worker/         # background worker (email, cron billing)
internal/
  auth/           # registrasi, login, token, verifikasi, reset password
  user/           # profil, ganti email/password, hapus akun
  org/            # organisasi, membership, undangan, role
  billing/        # plan, subscription, invoice, webhook
  entitlement/    # feature gating & limit
  mail/           # template & pengiriman email
  platform/       # db, config, logger, http middleware, errors, clock
migrations/
templates/email/
```

Aturan: modul hanya boleh saling memanggil lewat interface/service publik, bukan mengakses tabel modul lain secara langsung.

---

## 3. Konvensi API

- Base path: `/api/v1`
- Format: JSON
- Autentikasi: `Authorization: Bearer <access_token>`
- Resource milik organisasi selalu di bawah `/orgs/{orgId}/...`

### Format error

```json
{
  "error": {
    "code": "PLAN_LIMIT_REACHED",
    "message": "Paket Free hanya mendukung 3 anggota.",
    "details": { "limit": "members", "max": 3 }
  }
}
```

| HTTP | Kode umum |
|---|---|
| 400 | `VALIDATION_ERROR` |
| 401 | `UNAUTHENTICATED`, `TOKEN_EXPIRED` |
| 403 | `FORBIDDEN`, `EMAIL_NOT_VERIFIED`, `PLAN_FEATURE_UNAVAILABLE` |
| 402 | `PLAN_LIMIT_REACHED`, `SUBSCRIPTION_INACTIVE` |
| 404 | `NOT_FOUND` |
| 409 | `CONFLICT` (mis. email sudah terdaftar) |
| 429 | `RATE_LIMITED` |

### Pagination

Cursor-based: `?limit=20&cursor=<opaque>`. Respons menyertakan `next_cursor`.

---

## 4. Model Data

### 4.1 `users`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| email | citext UNIQUE | case-insensitive |
| password_hash | text | Argon2id |
| name | text | |
| avatar_url | text NULL | |
| email_verified_at | timestamptz NULL | |
| status | text | `active`, `suspended`, `deleted` |
| created_at, updated_at | timestamptz | |
| deleted_at | timestamptz NULL | soft delete |

### 4.2 `user_tokens`

Satu tabel untuk semua token sekali pakai.

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| user_id | uuid FK | |
| type | text | `email_verification`, `password_reset`, `email_change` |
| token_hash | text UNIQUE | SHA-256 dari token mentah |
| payload | jsonb NULL | mis. `{ "new_email": "..." }` |
| expires_at | timestamptz | |
| used_at | timestamptz NULL | |
| created_at | timestamptz | |

Masa berlaku: verifikasi email 24 jam, reset password 1 jam, ganti email 24 jam.

### 4.3 `refresh_tokens`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| user_id | uuid FK | |
| family_id | uuid | satu login = satu family |
| token_hash | text UNIQUE | |
| user_agent, ip | text | |
| expires_at | timestamptz | |
| revoked_at | timestamptz NULL | |
| replaced_by | uuid NULL | token pengganti setelah rotasi |
| created_at | timestamptz | |

### 4.4 `organizations`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| name | text | |
| slug | citext UNIQUE | untuk URL |
| logo_url | text NULL | |
| owner_id | uuid FK users | tepat satu owner |
| created_at, updated_at | timestamptz | |
| deleted_at | timestamptz NULL | |

### 4.5 `memberships`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| organization_id | uuid FK | |
| user_id | uuid FK | |
| role | text | `owner`, `admin`, `member` |
| created_at | timestamptz | |

Constraint: `UNIQUE (organization_id, user_id)`.

### 4.6 `invitations`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| organization_id | uuid FK | |
| email | citext | |
| role | text | `admin` atau `member` |
| token_hash | text UNIQUE | |
| invited_by | uuid FK users | |
| expires_at | timestamptz | 7 hari |
| accepted_at, declined_at, revoked_at | timestamptz NULL | |
| created_at | timestamptz | |

Constraint: hanya satu undangan aktif per `(organization_id, email)`.

### 4.7 `plans`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| code | text UNIQUE | `free`, `pro`, `business` |
| name | text | |
| price_amount | bigint | 0 untuk Free |
| currency | text | `IDR` |
| interval | text | `month`, `year` |
| trial_days | int | |
| features | jsonb | mis. `["audit_log", "export_csv"]` |
| limits | jsonb | mis. `{ "members": 3, "projects": 5 }`, `-1` = tak terbatas |
| is_active | bool | plan lama bisa dinonaktifkan tanpa dihapus |
| sort_order | int | |

### 4.8 `subscriptions`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| organization_id | uuid FK UNIQUE | satu subscription per organisasi |
| plan_id | uuid FK | |
| status | text | lihat state machine §8.2 |
| trial_ends_at | timestamptz NULL | |
| current_period_start | timestamptz | |
| current_period_end | timestamptz | |
| cancel_at_period_end | bool | |
| pending_plan_id | uuid NULL | untuk downgrade terjadwal |
| grace_until | timestamptz NULL | |
| created_at, updated_at | timestamptz | |

### 4.9 `invoices`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| organization_id | uuid FK | |
| subscription_id | uuid FK | |
| number | text UNIQUE | mis. `INV-2026-000123` |
| plan_id | uuid FK | snapshot plan saat ditagih |
| amount | bigint | |
| currency | text | |
| status | text | `draft`, `open`, `paid`, `expired`, `void` |
| period_start, period_end | timestamptz | |
| due_at | timestamptz | |
| paid_at | timestamptz NULL | |
| provider | text | `midtrans` / `xendit` |
| provider_ref | text NULL | ID transaksi di gateway |
| payment_url | text NULL | |
| created_at, updated_at | timestamptz | |

### 4.10 `payment_events`

Untuk idempotensi webhook.

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| provider | text | |
| event_id | text | ID unik dari gateway |
| payload | jsonb | data mentah |
| received_at | timestamptz | |
| processed_at | timestamptz NULL | |
| error | text NULL | |

Constraint: `UNIQUE (provider, event_id)`.

### 4.11 `outbox_jobs`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| type | text | mis. `email.send` |
| payload | jsonb | |
| status | text | `pending`, `processing`, `done`, `failed` |
| attempts | int | |
| next_attempt_at | timestamptz | |
| last_error | text NULL | |
| created_at, updated_at | timestamptz | |

Job dimasukkan dalam **transaksi yang sama** dengan perubahan data, supaya tidak ada email yang terkirim untuk data yang gagal disimpan (atau sebaliknya).

---

## 5. Modul Auth

### 5.1 Endpoint

| Method | Path | Auth | Fungsi |
|---|---|---|---|
| POST | `/auth/register` | – | Daftar user + buat organisasi pertama |
| POST | `/auth/login` | – | Login, dapat access + refresh token |
| POST | `/auth/refresh` | – | Tukar refresh token (rotasi) |
| POST | `/auth/logout` | ✓ | Cabut refresh token saat ini |
| POST | `/auth/verify-email` | – | Verifikasi dengan token |
| POST | `/auth/resend-verification` | ✓ | Kirim ulang email verifikasi |
| POST | `/auth/forgot-password` | – | Kirim email reset |
| POST | `/auth/reset-password` | – | Set password baru dengan token |

### 5.2 Aturan bisnis

- Registrasi menerima `email`, `password`, `name`, `organization_name`. Dalam satu transaksi: buat user, organisasi, membership `owner`, subscription plan `free` (atau trial), dan job email verifikasi.
- Password minimal 8 karakter, maksimal 72. Tolak password yang sama dengan email.
- User yang belum verifikasi email **boleh login** tetapi aksi tertentu diblokir dengan `EMAIL_NOT_VERIFIED` (mengundang anggota, checkout).
- `forgot-password` **selalu** merespons 200 meskipun email tidak terdaftar (cegah user enumeration).
- Reset password berhasil → semua refresh token user dicabut.
- Token sekali pakai: setelah dipakai, `used_at` diisi dan tidak bisa dipakai lagi.

### 5.3 Refresh token rotation

1. Setiap `/auth/refresh` mengeluarkan refresh token baru dan mencabut yang lama (`replaced_by` diisi).
2. Jika token yang **sudah dicabut** dipakai lagi → anggap dicuri → cabut seluruh `family_id`.
3. Refresh token dikirim lewat cookie `HttpOnly; Secure; SameSite=Strict` untuk klien web, atau body untuk klien mobile.

### 5.4 Rate limit

| Endpoint | Batas |
|---|---|
| `/auth/login` | 5 percobaan gagal / 15 menit per (email + IP) |
| `/auth/forgot-password` | 3 / jam per email |
| `/auth/resend-verification` | 3 / jam per user |
| `/auth/register` | 10 / jam per IP |

### 5.5 Klaim JWT

```json
{ "sub": "<user_id>", "sid": "<refresh_family_id>", "iat": 0, "exp": 0 }
```

Role dan organisasi **tidak** disimpan di JWT. Dicek dari database per request supaya perubahan role langsung berlaku.

---

## 6. Modul User

| Method | Path | Fungsi |
|---|---|---|
| GET | `/me` | Profil + daftar organisasi yang diikuti |
| PATCH | `/me` | Ubah nama, avatar |
| POST | `/me/password` | Ganti password (wajib password lama) |
| POST | `/me/email` | Minta ganti email (kirim verifikasi ke email baru) |
| POST | `/me/email/confirm` | Konfirmasi email baru dengan token |
| GET | `/me/sessions` | Daftar sesi aktif |
| DELETE | `/me/sessions/{id}` | Cabut satu sesi |
| DELETE | `/me` | Hapus akun |

### Aturan

- Ganti password → cabut semua sesi lain kecuali sesi saat ini.
- Ganti email → email lama menerima notifikasi bahwa ada perubahan.
- Hapus akun **ditolak** jika user masih menjadi owner organisasi yang punya anggota lain. User harus transfer kepemilikan dulu. Jika owner satu-satunya di organisasinya, organisasi ikut dihapus (soft delete) dan subscription dibatalkan.
- Hapus akun = soft delete + anonimisasi email (`deleted+<id>@invalid`) supaya email bisa dipakai daftar ulang.

---

## 7. Modul Organisasi, Tim & Role

### 7.1 Endpoint

| Method | Path | Fungsi |
|---|---|---|
| GET | `/orgs` | Organisasi milik user |
| POST | `/orgs` | Buat organisasi baru |
| GET | `/orgs/{orgId}` | Detail |
| PATCH | `/orgs/{orgId}` | Ubah nama, slug, logo |
| DELETE | `/orgs/{orgId}` | Hapus organisasi |
| GET | `/orgs/{orgId}/members` | Daftar anggota |
| PATCH | `/orgs/{orgId}/members/{userId}` | Ubah role |
| DELETE | `/orgs/{orgId}/members/{userId}` | Keluarkan anggota |
| POST | `/orgs/{orgId}/leave` | Keluar dari organisasi |
| POST | `/orgs/{orgId}/transfer-ownership` | Pindahkan owner |
| GET | `/orgs/{orgId}/invitations` | Daftar undangan aktif |
| POST | `/orgs/{orgId}/invitations` | Undang via email |
| DELETE | `/orgs/{orgId}/invitations/{id}` | Batalkan undangan |
| POST | `/orgs/{orgId}/invitations/{id}/resend` | Kirim ulang |
| GET | `/invitations/{token}` | Info undangan (publik) |
| POST | `/invitations/{token}/accept` | Terima (wajib login) |
| POST | `/invitations/{token}/decline` | Tolak |

### 7.2 Matriks izin

| Aksi | Owner | Admin | Member |
|---|:-:|:-:|:-:|
| Lihat organisasi & anggota | ✓ | ✓ | ✓ |
| Ubah pengaturan organisasi | ✓ | ✓ | – |
| Undang / batalkan undangan | ✓ | ✓ | – |
| Ubah role member ↔ admin | ✓ | ✓* | – |
| Keluarkan anggota | ✓ | ✓* | – |
| Kelola billing | ✓ | – | – |
| Transfer kepemilikan | ✓ | – | – |
| Hapus organisasi | ✓ | – | – |
| Keluar dari organisasi | –** | ✓ | ✓ |

\* Admin tidak bisa mengubah atau mengeluarkan admin lain maupun owner.
\** Owner harus transfer kepemilikan dulu.

### 7.3 Tenant context middleware

Untuk setiap route `/orgs/{orgId}/...`:

1. Ambil `user_id` dari JWT.
2. Cari membership `(orgId, user_id)`. Tidak ada → `404` (bukan 403, supaya keberadaan organisasi tidak bocor).
3. Simpan `orgId`, `role` ke `context.Context`.
4. Repository modul domain **wajib** mengambil `orgId` dari context, bukan dari input user.

Opsional (direkomendasikan): aktifkan PostgreSQL Row Level Security dengan `SET LOCAL app.current_org = '<orgId>'` per transaksi sebagai lapisan pengaman kedua.

### 7.4 Aturan undangan

- Hanya bisa mengundang email yang belum menjadi anggota.
- Mengundang email yang punya undangan aktif → perbarui undangan lama (token baru, masa berlaku baru).
- Undangan dihitung terhadap limit `members` (anggota + undangan aktif).
- Menerima undangan: email user yang login harus sama dengan email undangan.
- Jika penerima belum punya akun, halaman undangan mengarahkan ke registrasi. Setelah daftar dengan email yang sama, undangan otomatis diterima, dan **tidak** dibuatkan organisasi baru.

### 7.5 Transfer kepemilikan

- Target harus anggota organisasi.
- Dalam satu transaksi: target jadi `owner`, owner lama jadi `admin`, `organizations.owner_id` diperbarui.
- Wajib konfirmasi password owner.

---

## 8. Modul Billing & Subscription

### 8.1 Model penagihan

Gateway lokal umumnya tidak menyediakan subscription berulang yang lengkap, jadi **mesin langganan dikelola sendiri**:

1. Sistem membuat `invoice` untuk setiap periode.
2. Invoice dibayar lewat payment link/checkout gateway.
3. Webhook gateway menandai invoice `paid`, lalu subscription diperpanjang.

Interface gateway:

```go
type PaymentProvider interface {
    CreatePayment(ctx context.Context, inv Invoice, customer Customer) (PaymentSession, error)
    VerifyWebhook(r *http.Request) (WebhookEvent, error) // validasi signature
    GetPaymentStatus(ctx context.Context, providerRef string) (PaymentStatus, error)
}
```

### 8.2 State machine subscription

```
           ┌──────────┐  trial habis, belum bayar   ┌──────────┐
 daftar ──▶│ trialing │────────────────────────────▶│ expired  │
           └────┬─────┘                             └──────────┘
                │ bayar                                  ▲
                ▼                                        │ grace habis
           ┌──────────┐  periode habis, invoice belum ┌──┴───────┐
           │  active  │──────────────────────────────▶│ past_due │
           └────┬─────┘◀──────────── bayar ───────────└──────────┘
                │ cancel_at_period_end & periode habis
                ▼
           ┌──────────┐
           │ canceled │
           └──────────┘
```

| Status | Akses fitur berbayar |
|---|---|
| `trialing` | Ya |
| `active` | Ya |
| `past_due` | Ya, selama `grace_until` (default 3 hari) |
| `expired` / `canceled` | Tidak. Organisasi kembali ke limit plan `free` |

Data **tidak dihapus** saat langganan berakhir. Hanya akses yang dibatasi.

### 8.3 Endpoint

| Method | Path | Fungsi |
|---|---|---|
| GET | `/plans` | Daftar plan aktif (publik) |
| GET | `/orgs/{orgId}/subscription` | Status langganan + pemakaian vs limit |
| POST | `/orgs/{orgId}/subscription/checkout` | Buat invoice + payment link untuk plan tertentu |
| POST | `/orgs/{orgId}/subscription/change-plan` | Upgrade / downgrade |
| POST | `/orgs/{orgId}/subscription/cancel` | Set `cancel_at_period_end = true` |
| POST | `/orgs/{orgId}/subscription/resume` | Batalkan pembatalan |
| GET | `/orgs/{orgId}/invoices` | Riwayat invoice |
| GET | `/orgs/{orgId}/invoices/{id}` | Detail invoice |
| POST | `/webhooks/{provider}` | Webhook pembayaran |

### 8.4 Aturan perubahan plan (v1, tanpa proration)

- **Upgrade:** buat invoice plan baru. Setelah dibayar, plan berganti dan periode baru dimulai saat itu juga. Sisa periode lama tidak dikreditkan.
- **Downgrade:** dijadwalkan lewat `pending_plan_id`, berlaku di akhir periode. Jika pemakaian melebihi limit plan baru, tampilkan peringatan; data tetap ada, tetapi pembuatan resource baru diblokir.
- Proration dicatat sebagai kandidat Tier 2.

### 8.5 Webhook

1. Verifikasi signature. Gagal → `401`, jangan diproses.
2. Simpan ke `payment_events`. Jika `(provider, event_id)` sudah ada → langsung `200` (idempoten).
3. Proses dalam transaksi: update invoice → update subscription → masukkan job email.
4. Isi `processed_at`. Jika gagal, simpan `error` dan kembalikan `500` supaya gateway mengirim ulang.
5. Jangan pernah percaya nominal dari redirect klien. Status pembayaran hanya dari webhook atau `GetPaymentStatus`.

### 8.6 Cron billing (worker)

| Job | Jadwal | Fungsi |
|---|---|---|
| `billing.renewals` | tiap jam | Buat invoice untuk subscription yang periodenya habis dalam 3 hari |
| `billing.expire` | tiap jam | `active` → `past_due` saat periode habis tanpa pembayaran; `past_due` → `expired` saat grace habis; `trialing` → `expired` saat trial habis |
| `billing.apply_scheduled` | tiap jam | Terapkan `pending_plan_id` dan `cancel_at_period_end` |
| `billing.reminders` | harian | Pengingat H-3 dan H-1 jatuh tempo |
| `billing.reconcile` | harian | Cek status invoice `open` langsung ke gateway (antisipasi webhook hilang) |

Semua job harus aman dijalankan ulang (idempoten) dan aman dijalankan bersamaan (pakai `SELECT ... FOR UPDATE SKIP LOCKED` atau advisory lock).

---

## 9. Modul Entitlement (Feature Gating & Limit)

### 9.1 Interface

```go
type Entitlements interface {
    HasFeature(ctx context.Context, orgID uuid.UUID, feature string) (bool, error)
    CheckLimit(ctx context.Context, orgID uuid.UUID, limit string, delta int) error // PLAN_LIMIT_REACHED jika melebihi
    Usage(ctx context.Context, orgID uuid.UUID) (map[string]LimitUsage, error)
}
```

### 9.2 Aturan

- Plan efektif = plan subscription jika status memberi akses (§8.2), selain itu plan `free`.
- Pemakaian dihitung dari data aktual (mis. `COUNT` membership), bukan counter terpisah, supaya tidak pernah tidak sinkron.
- Limit dicek **di service layer** sebelum membuat resource, di dalam transaksi yang sama.
- Middleware `RequireFeature("audit_log")` tersedia untuk melindungi route.
- Respons error menyertakan limit dan plan yang dibutuhkan, supaya frontend bisa menampilkan ajakan upgrade.

### 9.3 Contoh konfigurasi plan awal

| | Free | Pro | Business |
|---|---|---|---|
| Harga / bulan | Rp0 | Rp99.000 | Rp299.000 |
| Trial | – | 14 hari | 14 hari |
| `members` | 3 | 10 | -1 |
| Features | – | `export_csv` | `export_csv`, `audit_log` |

Angka hanya contoh. Plan disimpan di database lewat seed migration, bukan di-hardcode.

---

## 10. Modul Email Transaksional

### 10.1 Arsitektur

```
service (dalam transaksi) ──insert──▶ outbox_jobs ──▶ worker ──▶ Mailer (SMTP / API provider)
```

- Interface `Mailer` supaya provider bisa diganti; implementasi dev menulis ke log atau Mailpit.
- Retry dengan exponential backoff: 1m, 5m, 30m, 2j, 6j. Setelah 5 kali → `failed` dan dicatat untuk dicek manual.
- Template HTML + plain text, ditulis dengan `html/template`.

### 10.2 Template

| Kode | Pemicu |
|---|---|
| `verify_email` | Registrasi, kirim ulang verifikasi |
| `password_reset` | Lupa password |
| `password_changed` | Password berhasil diganti/direset |
| `email_change_confirm` | Permintaan ganti email (ke email baru) |
| `email_changed_notice` | Email berhasil diganti (ke email lama) |
| `invitation` | Undangan anggota |
| `invoice_created` | Invoice baru terbit |
| `payment_succeeded` | Pembayaran berhasil |
| `payment_reminder` | H-3, H-1 jatuh tempo |
| `subscription_past_due` | Masuk masa tenggang |
| `subscription_expired` | Langganan berakhir |
| `trial_ending` | 3 hari sebelum trial habis |

---

## 11. Kebutuhan Non-Fungsional

- **Keamanan:** HTTPS wajib, header keamanan standar, CORS terbatas, semua input divalidasi, query parametrized, secret dari environment variable.
- **Observability:** structured logging (JSON) dengan `request_id`, `user_id`, `org_id`; endpoint `/healthz` dan `/readyz`.
- **Konsistensi:** semua operasi multi-tabel dalam satu transaksi.
- **Testing:** unit test untuk service layer; integration test dengan PostgreSQL asli (testcontainers) untuk repository, webhook, dan isolasi tenant.
- **Seed data:** perintah untuk membuat plan default dan akun demo di environment dev.

---

## 12. Kriteria Selesai (Acceptance Criteria)

### Auth
- [ ] User bisa daftar, otomatis punya organisasi dan plan Free
- [ ] Verifikasi email, lupa password, dan reset password berjalan end-to-end
- [ ] Refresh token dirotasi; pemakaian ulang token lama mencabut seluruh family
- [ ] Rate limit login aktif
- [ ] `forgot-password` tidak membocorkan apakah email terdaftar

### User
- [ ] Ganti password mencabut sesi lain
- [ ] Ganti email butuh konfirmasi dari email baru
- [ ] Hapus akun ditolak jika masih owner organisasi dengan anggota lain

### Multi-tenancy & Tim
- [ ] User di organisasi A tidak bisa membaca atau mengubah data organisasi B (ada test-nya)
- [ ] Undangan bisa dikirim, diterima, ditolak, dibatalkan, dan kedaluwarsa
- [ ] Matriks izin §7.2 tercakup test
- [ ] Transfer kepemilikan berjalan atomik

### Billing
- [ ] Checkout menghasilkan payment link yang valid di sandbox gateway
- [ ] Webhook yang sama dikirim dua kali hanya diproses sekali
- [ ] Webhook dengan signature salah ditolak
- [ ] Transisi state machine §8.2 tercakup test (dengan clock yang bisa dimanipulasi)
- [ ] Cron reconcile memperbaiki invoice yang webhook-nya hilang

### Entitlement
- [ ] Menambah anggota melebihi limit menghasilkan `PLAN_LIMIT_REACHED`
- [ ] Organisasi `expired` otomatis kembali ke limit Free

### Email
- [ ] Semua email dikirim lewat outbox, tidak ada pengiriman sinkron di request
- [ ] Job yang gagal di-retry sesuai jadwal backoff

---

## 13. Di Luar Cakupan Tier 1

Panel super admin, audit log, notifikasi in-app, 2FA, OAuth login, upload file, proration, kupon/diskon, pajak di invoice, multi-currency, API publik.

---

## 14. Keputusan Terbuka

| # | Pertanyaan | Default sementara |
|---|---|---|
| 1 | Gateway pertama: Midtrans atau Xendit? | Pilih satu, lewat interface |
| 2 | Registrasi langsung trial Pro atau Free? | Free, trial dipicu saat pilih plan |
| 3 | Pakai RLS PostgreSQL sejak awal? | Ya, sebagai lapisan kedua |
| 4 | Frontend: SPA terpisah atau server-rendered? | Belum ditentukan; API dirancang agnostik |
| 5 | Penomoran invoice per organisasi atau global? | Global |
