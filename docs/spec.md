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
| State sementara | Redis (satu instance bersama): rate limit, cache. Bukan sumber kebenaran; data penting tetap di PostgreSQL |
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
- Setiap respons menyertakan header `X-Request-ID` dan `X-Trace-ID` (lihat §11.1).

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
| 502 | `PAYMENT_PROVIDER_ERROR` (gateway pembayaran tidak terjangkau atau menolak; tidak ada yang tersimpan, boleh dicoba lagi) |
| 500 | `INTERNAL_ERROR` (pesan generik, penyebab hanya masuk log) |

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
| current_period_end | timestamptz NULL | `NULL` = periode tak berakhir (plan Free, tidak pernah ditagih) |
| cancel_at_period_end | bool | |
| pending_plan_id | uuid NULL | untuk downgrade terjadwal |
| grace_until | timestamptz NULL | |
| created_at, updated_at | timestamptz | |

Catatan implementasi (Fase 5): `current_period_end` boleh `NULL` supaya subscription Free tidak berpura-pura punya periode; job billing (Fase 6) hanya memilih baris dengan `current_period_end IS NOT NULL`. Subscription Free berstatus `active` dengan plan `free`. `status` dibatasi CHECK pada lima nilai §8.2, dan `trialing` mewajibkan `trial_ends_at`. Tabel ini ber-RLS (`tenant_isolation`) seperti tabel org-scoped lain; migrasi mengisi subscription Free untuk setiap organisasi yang belum dihapus. `trial_ends_at` yang terisi berarti trial organisasi itu sudah dipakai (§8.7).

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
| kind | text | `checkout` (pembelian atas permintaan organisasi; periode berbayar mulai saat dibayar) atau `renewal` (periode berikutnya, dibuat job); tambahan Fase 6, lihat §8.7 |
| reminder_3d_sent_at, reminder_1d_sent_at | timestamptz NULL | penanda pengingat yang sudah terkirim (§8.7) |
| created_at, updated_at | timestamptz | |

Catatan implementasi (Fase 6): tabel ini ber-RLS (`tenant_isolation`) seperti tabel org-scoped lain. Dua indeks unik parsial menjaga idempotensi: satu checkout hidup (`draft`/`open`) per subscription, dan satu invoice `renewal` bukan-`void` per `(subscription_id, period_start)`. Nomor diambil dari sequence PostgreSQL global `invoice_number_seq`; sequence tidak ikut di-rollback, jadi transaksi yang gagal bisa meninggalkan celah dalam penomoran (tidak apa-apa; yang tidak boleh adalah nomor yang dipakai dua kali).

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

`processed_at` kosong berarti belum diproses atau gagal (`error` berisi sebabnya; kiriman ulang gateway memprosesnya lagi). `processed_at` terisi dengan `error` terisi berarti selesai diproses tetapi perlu dilihat manusia (mis. pembayaran untuk invoice yang sudah `void`). Tabel ini global (tanpa `organization_id`, tanpa RLS): webhook belum punya tenant sebelum dicocokkan dengan invoice.

### 4.11 `outbox_jobs`

| Kolom | Tipe | Catatan |
|---|---|---|
| id | uuid PK | |
| type | text | mis. `email.send` |
| payload | jsonb | |
| status | text | `pending`, `processing`, `done`, `failed` |
| attempts | int | |
| next_attempt_at | timestamptz | |
| locked_at | timestamptz NULL | diisi saat worker mengambil job; job `processing` yang lease-nya lewat dianggap worker mati dan diambil ulang |
| last_error | text NULL | |
| trace_id | text NULL | `trace_id` dari request asal, dipulihkan oleh worker (§11.1) |
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

### 5.6 Keputusan implementasi

Hal-hal yang tidak ditentukan §5.1–5.5 dan sudah diputuskan di Fase 2:

- **Kepemilikan tabel.** Modul `user` memiliki `users`; modul `auth` memiliki `user_tokens` dan `refresh_tokens` dan memanggil `user` lewat interface. Fungsi repository menerima `db.DBTX` (pool atau transaksi) sehingga registrasi, token, dan job email bisa berada dalam satu transaksi.
- **Register** menerima `email`, `password`, `name`, `organization_name`. `organization_name` wajib; dalam transaksi yang sama dengan user dibuat organisasi dengan nama itu dan membership `owner` (`org.Service.CreateWithOwner`, lewat interface `auth.OrganizationProvisioner`). Slug diturunkan dari nama (`acme-corp`); bila sudah dipakai ditambah akhiran acak 6 hex (`acme-3f9a1c`). Subscription `free` ditambahkan di Fase 5. Register **tidak** mengembalikan token; klien lanjut ke `/auth/login`. Email duplikat → `409 CONFLICT`.
- **Login** menerima `email`, `password`, dan opsional `token_delivery` (`cookie` default, atau `body`). Respons: `access_token`, `token_type` (`Bearer`), `expires_in` (detik), `user`, dan `refresh_token` hanya bila `token_delivery=body`. Dengan `cookie`, refresh token dikirim lewat cookie `refresh_token` (`HttpOnly`, `SameSite=Strict`, `Path=/api/v1/auth`, `Secure` di production). Email tidak dikenal, password salah, dan user dihapus/ditangguhkan memberi respons sama (`401 UNAUTHENTICATED`, pesan generik) dan waktu proses yang mirip (hash tiruan untuk email tak dikenal).
- **Refresh** membaca token dari cookie, atau dari body `{ "refresh_token": "..." }`. Cara token datang menentukan cara token baru dikembalikan (cookie atau body). Token tak dikenal/kedaluwarsa → `401 UNAUTHENTICATED`; token yang sudah dicabut dipakai lagi → cabut seluruh family lalu `401`. Dua permintaan refresh bersamaan dengan token yang sama dianggap pemakaian ulang (sesuai §5.3); klien web sebaiknya menserialisasi refresh.
- **Sesi.** Middleware autentikasi memverifikasi JWT, memastikan user `active` (dibaca dari cache Redis, lihat "Cache member"), dan memastikan family `sid` masih punya refresh token yang belum dicabut dan belum kedaluwarsa (selalu dari DB). Dengan begitu logout, reset password, dan deteksi pencurian berlaku langsung, tidak menunggu access token habis. `TOKEN_EXPIRED` dikembalikan untuk access token yang kedaluwarsa.
- **Cache member (Redis).** Data member yang dibaca di jalur panas (autentikasi tiap request, `refresh`, `resend-verification`) di-cache sebagai *summary* (`id`, `email`, `name`, `avatar_url`, `email_verified_at`, `status`; **tanpa password hash**) di key `<key_prefix>:user:summary:v1:<user_id>` dengan TTL `redis.user_cache_ttl` (default 5 menit). Pola cache-aside: miss → baca DB → isi cache; data yang tidak ditemukan tidak di-cache; Redis mati atau entri rusak → jatuh ke DB (cache bukan titik gagal). **Invalidasi otomatis di lapisan service:** setiap method tulis di `user.Service` (ubah profil, ganti email, verifikasi email, suspend, hapus akun, ...) mengakhiri dirinya dengan `invalidateAfterCommit`, yang mendaftarkan penghapusan cache lewat hook `db.AfterCommit`: dijalankan **setelah** transaksi commit, tidak pernah saat rollback, dan langsung bila penulisan tidak memakai transaksi. (Menghapus sebelum commit membiarkan pembaca mengisi ulang cache dengan data lama.) Endpoint update/hapus member cukup memanggil method service; tidak ada langkah cache yang bisa terlupa. Tidak ada endpoint HTTP untuk menghapus cache. `user.Service.InvalidateCache(ids...)` tetap tersedia sebagai fungsi untuk kasus khusus. Bila invalidasi gagal (Redis mati), error dicatat dan entri basi paling lama satu TTL, sehingga efek suspend/hapus akun bisa tertunda sampai TTL itu habis. Pembacaan di dalam transaksi memakai `GetByID` (langsung DB), bukan cache. Login memakai DB karena butuh password hash.
- **Logout** mencabut seluruh family sesi saat ini dan menghapus cookie.
- **Resend verification** untuk user yang sudah terverifikasi → `409 CONFLICT`. Mengirim ulang membuat token baru dan membatalkan token verifikasi lama yang belum dipakai.
- **Forgot password** selalu `200` dengan pesan generik. Token reset lama yang belum dipakai dibatalkan saat token baru dibuat. Pembuatan token dan job email asinkron, jadi perbedaan waktu antara email terdaftar dan tidak sangat kecil; rate limit per email menutup sisanya.
- **Rate limit** disimpan di Redis (sorted set per key, sliding window, dievaluasi atomik dengan skrip Lua) sehingga konsisten antar instance API dan cepat. Key berisi hash SHA-256 dari email/IP, bukan nilai aslinya, dan kedaluwarsa otomatis. Bila Redis tidak dapat dihubungi, endpoint yang dibatasi gagal tertutup (500) alih-alih melewati pembatasan. Melebihi batas → `429 RATE_LIMITED` dengan header `Retry-After`. Batas login dihitung dari percobaan **gagal** dan direset saat login berhasil. Batas `forgot-password` berlaku sama untuk email terdaftar maupun tidak.
- **Redis:** satu instance dipakai bersama banyak modul; semua key diawali `<redis.key_prefix>:<modul>:`. `/readyz` ikut memeriksa Redis. Konfigurasi: section `redis` (`addr`, `password`, `db`, `key_prefix`).
- **Konfigurasi baru:** `auth.jwt_secret` (minimal 32 karakter, wajib), `auth.access_token_ttl` (default 15m), `auth.refresh_token_ttl` (default 720h), `app.frontend_url` (dasar tautan di email: `<frontend_url>/verify-email?token=...`, `<frontend_url>/reset-password?token=...`).

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

### 6.1 Keputusan implementasi

Hal-hal yang tidak ditentukan §6 dan sudah diputuskan di Fase 3:

- **Pembagian modul.** Modul `user` melayani semua endpoint `/me` (`user.Handler`) dan memiliki tabel `users`. Password, token sekali pakai, dan sesi dimiliki modul `auth` yang sudah bergantung pada `user`, jadi `user` tidak boleh mengimpor `auth`. Solusinya: `user` mendeklarasikan interface `AccountSecurity` (ganti password, ganti email, sesi, hapus akun) dan `auth.Service` mengimplementasikannya; `internal/app` yang menyambungkan keduanya. Handler mengambil ID user dan sesi dari `context.Context` (diisi middleware autentikasi), bukan dari input.
- **`PATCH /me`** hanya menerima `name` (1–100 karakter) dan `avatar_url` (URL `http(s)` absolut, maks. 2048 karakter; `null` menghapus avatar). Field lain, termasuk `email`, ditolak sebagai field tak dikenal. Body kosong ditolak. Upload file tidak ada di Tier 1, jadi avatar hanya berupa URL.
- **Konfirmasi password lama** diwajibkan untuk tindakan sensitif: ganti password (spec), dan juga ganti email serta hapus akun (tambahan, tidak ada di tabel §6). Tanpa itu, access token yang dicuri cukup untuk mengganti email lalu mengambil alih akun lewat `forgot-password`. Password lama yang salah → `400 VALIDATION_ERROR` pada field `current_password` (bukan 401, karena klien memperlakukan 401 sebagai "login ulang"). Percobaan **gagal** dibatasi 5 per 15 menit per user (`RulePasswordConfirm`, Redis); selama terkunci, password yang benar pun ditolak dengan `429`. Berhasil → hitungan direset.
- **`POST /me/password`** menerima `current_password` dan `new_password`. Password baru mengikuti kebijakan §5.2 dan harus berbeda dari yang lama; kesalahan dilaporkan pada field `new_password`. Dalam satu transaksi: simpan hash baru, cabut semua sesi **kecuali sesi saat ini** (`sid` dari access token), dan antre email `password_changed`.
- **Ganti email** dua langkah. `POST /me/email` (`new_email`, `current_password`) memeriksa password, lalu mengirim `email_change_confirm` ke **email baru** dengan tautan `<frontend_url>/confirm-email-change?token=...` (24 jam, payload `{"new_email": "..."}`); email akun belum berubah. Permintaan baru membatalkan tautan lama. Ditolak bila sama dengan email sekarang (`400`, field `new_email`) atau sudah dipakai akun lain (`409 CONFLICT`). Dibatasi 3 permintaan per jam per user (`RuleEmailChange`) dan dihitung **sebelum** pemeriksaan "email sudah terdaftar", agar endpoint ini tidak bisa dipakai menebak email terdaftar tanpa batas. `POST /me/email/confirm` (`token`) **tidak butuh login**: tautan dibuka dari inbox baru, mungkin di perangkat lain, jadi token sekali pakai itulah kredensialnya (sama seperti `verify-email`). Dalam satu transaksi: pakai token, ganti email (otomatis terverifikasi, karena tautan sampai ke alamat itu), antre `email_changed_notice` ke **email lama** yang menyebut alamat barunya. Alamat yang keburu dipakai orang lain → `409` dan token tidak terpakai. Sesi tidak dicabut.
- **Sesi.** Sesi adalah satu login (satu `family_id` refresh token); ID-nya sama dengan klaim `sid`, dan stabil walau token dirotasi. `GET /me/sessions` mengembalikan `{"sessions": [...]}` berisi `id`, `user_agent`, `ip`, `created_at` (saat login), `last_used_at` (rotasi terakhir), `expires_at`, dan `current` (sesi yang membuat request), diurutkan dari yang terakhir dipakai. Hanya sesi yang belum dicabut dan belum kedaluwarsa; maksimum 100 (tanpa paginasi karena dibatasi per user). `DELETE /me/sessions/{id}` → `204`; ID yang bukan milik user, tidak ada, sudah berakhir, atau bukan UUID semuanya `404 NOT_FOUND` (ID tidak bisa ditebak). Mencabut sesi saat ini diperbolehkan dan berarti logout; cookie refresh-nya mati sendiri saat dipakai berikutnya (`/auth/refresh` menghapus cookie yang ditolak).
- **Hapus akun.** `DELETE /me` menerima `{"current_password": "..."}` di body (`204` bila berhasil). Dalam satu transaksi: modul organisasi dipanggil lewat interface `auth.OrganizationGuard.BeforeUserDeleted` (menolak dengan `409 CONFLICT`, `details.reason = "owns_shared_organization"`, bila user masih owner organisasi yang punya anggota lain; selain itu mengeluarkan user dari organisasinya dan menghapus (soft) organisasi yang hanya berisi dia beserta membatalkan subscription-nya), lalu soft delete user (`status = deleted`, `deleted_at`, email jadi `deleted+<id>@invalid`) dan semua sesi dicabut. Cek dan pembersihan berada di transaksi yang sama supaya tidak ada yang bergabung di antaranya; penolakan membatalkan seluruh transaksi. Cache profil dihapus setelah commit, jadi akun langsung berhenti bekerja. `org.Service` mengimplementasikan guard itu (`auth.NoOrganizations` sudah dihapus; tes dapat menyuntikkan guard lewat `app.Options.Organizations`). Hanya email yang dianonimkan (sesuai aturan di atas); nama dan avatar tetap tersimpan di baris yang di-soft-delete.

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

### 7.6 Keputusan implementasi

Hal-hal yang tidak ditentukan §7.1–7.5 dan sudah diputuskan di Fase 4:

- **Pembagian modul.** Modul `org` memiliki `organizations`, `memberships`, `invitations`. `org` mengimpor `user` (bukan sebaliknya): `user.Handler` mendeklarasikan `OrganizationLister` untuk daftar organisasi di `GET /me`, dan `org.Service` mengimplementasikannya.
- **Rute per organisasi.** Modul lain memasang rute `/orgs/{orgId}/...` lewat `org.TenantRoutes(rg, authenticate, members)`, yang sudah memuat middleware autentikasi dan tenant. Handler membaca organisasi dan role hanya dari `context.Context` (`reqctx.OrgID`, `org.RoleFrom`). `orgId` bukan UUID, organisasi tak dikenal, organisasi yang sudah dihapus, dan bukan anggota semuanya `404 NOT_FOUND`.
- **Otorisasi di service.** Method service org memanggil `org.Authorize(ctx, action)` sendiri (matriks §7.2 sebagai data di `org/authz.go`), jadi pemanggil non-HTTP pun terjaga; middleware `org.Require(action)` tersedia untuk gerbang rute (billing). Aturan target (`CanManageMember`): owner mengelola admin dan member, admin hanya member, tidak ada yang mengelola owner (kepemilikan hanya pindah lewat transfer). Role yang bisa diberikan hanya `admin` dan `member` (`Assignable`).
- **`GET /orgs`** berpaginasi cursor (`?limit=&cursor=`), diurutkan dari keanggotaan tertua, dan memuat `role` pemanggil. `POST /orgs` membuat organisasi dengan pemanggil sebagai owner (`201`), `slug` opsional (diturunkan dari nama bila kosong; slug eksplisit yang dipakai → `409`). Aturan slug: 3–48 karakter, huruf kecil/angka/tanda hubung tunggal; tidak peka huruf besar-kecil.
- **`PATCH /orgs/{orgId}`** menerima `name`, `slug`, `logo_url` (`null` menghapus; hanya URL `http(s)` absolut). Hanya owner dan admin. Body kosong atau field tak dikenal (mis. `owner_id`) ditolak.
- **`DELETE /orgs/{orgId}`** (owner saja) adalah soft delete: baris dan slug tetap tercadangkan, undangan terbuka dicabut dalam transaksi yang sama, dan semua anggota langsung kehilangan akses (404). Subscription dibatalkan dan invoice yang masih menunggu bayar di-`void` dalam transaksi yang sama (lewat `org.SubscriptionLifecycle.OrganizationDeleted`, diimplementasikan billing; Fase 6, §8.7): bila pembatalan gagal, organisasi tidak terhapus.

- **Anggota.** `GET /orgs/{orgId}/members` (semua anggota; berpaginasi cursor, owner paling awal) menggabungkan `memberships` dengan nama/email dari `user.Service.GetSummaries`, jadi `org` tidak membaca tabel `users`. `PATCH .../members/{userId}` (`{"role":"admin|member"}`) dan `DELETE .../members/{userId}` mengikuti `CanManageMember`: owner mengelola admin dan member, admin hanya member (admin boleh menaikkan member jadi admin, tetapi tidak menurunkan admin), tidak ada yang mengelola owner atau dirinya sendiri lewat endpoint ini. `role: "owner"` ditolak `400`. Target dikunci (`FOR UPDATE`) sehingga perubahan bersamaan berurutan; target yang bukan anggota atau ID tak valid → `404`. `POST .../leave` → `204`; owner → `403` dengan `details.reason = "owner_must_transfer_ownership"`. Role dibaca dari DB tiap request, jadi perubahan role atau pengeluaran anggota berlaku di request berikutnya tanpa login ulang.

- **Transfer kepemilikan.** `POST /orgs/{orgId}/transfer-ownership` menerima `{"user_id": "...", "current_password": "..."}` (owner saja; `204`). Password dicek lewat `org.PasswordConfirmer` (diimplementasikan `auth.Service.ConfirmPassword`, memakai batas 5 salah / 15 menit yang sama dengan tindakan sensitif lain) sebelum apa pun dikunci atau diubah. Dalam satu transaksi: kedua membership dikunci dengan urutan tetap (tanpa deadlock), pemilik lama diturunkan jadi `admin`, target dinaikkan jadi `owner`, dan `organizations.owner_id` diubah dengan compare-and-set terhadap owner lama. Dua transfer yang berebut tidak bisa sama-sama berhasil: yang kalah menemukan dirinya bukan owner lagi (`403`). Target diri sendiri → `400` (`user_id`), bukan anggota → `404`. Tidak ada email notifikasi di Tier 1.

- **Mengirim undangan.** `POST /orgs/{orgId}/invitations` (`{"email","role"}`; owner dan admin; role hanya `admin`/`member`) memakai guard `EMAIL_NOT_VERIFIED` pada pengundang, begitu juga `.../resend`, karena keduanya membuat layanan mengirim email. Email dinormalisasi (huruf kecil). Dalam satu transaksi: cek bukan anggota (`409` bila sudah), buat atau perbarui undangan terbuka untuk alamat itu (`201` bila baru, `200` bila memperbarui; baris sama, token dan masa berlaku 7 hari diganti, role dan pengundang ikut diperbarui, token lama mati), dan antre email `invitation` dengan tautan `<frontend_url>/accept-invitation?token=...`. Token mentah hanya ada di email; DB menyimpan hash SHA-256. Tidak ada email tanpa undangan dan sebaliknya (satu transaksi). `GET .../invitations` (owner/admin, berpaginasi cursor) hanya memuat undangan aktif (terbuka dan belum kedaluwarsa). `DELETE .../invitations/{id}` → `204` dan tautannya mati; undangan yang tidak ada, milik organisasi lain, atau sudah ditutup → `404`. `POST .../invitations/{id}/resend` memberi token dan masa berlaku baru (undangan yang kedaluwarsa boleh dikirim ulang; yang sudah diterima/ditolak/dibatalkan `404`).

- **Tautan undangan.** Tiga endpoint dialamatkan lewat token di tautan email, bukan lewat organisasi: `GET /invitations/{token}` (publik) mengembalikan `email`, `role`, `organization` (`name`, `logo_url`), `inviter_name`, `expires_at`, dan `account_exists` (frontend memilih login atau registrasi; pemegang token sudah memiliki alamat itu, jadi tidak ada yang bocor). `POST /invitations/{token}/decline` (publik, `204`). `POST /invitations/{token}/accept` (login **dan email terverifikasi**; `200` berisi organisasi dan role). Pencarian token lintas tenant berjalan di transaksi sistem (`WithSystemTx`). Tautan yang tidak lagi berlaku (kedaluwarsa tepat 7 hari, dicabut, sudah diterima/ditolak, organisasi dihapus) → `404` dengan `details.reason` (`expired`, `revoked`, `accepted`, `declined`, `organization_deleted`); token yang tidak dikenal → `404` tanpa alasan. Email user yang login harus sama dengan email undangan, selain itu `403` (`details.reason = "invitation_email_mismatch"`) dan undangan **tidak** terpakai. Undangan dikunci (`FOR UPDATE`) saat diterima/ditolak, jadi satu tautan hanya bisa dipakai sekali walau diklik bersamaan. Penerima yang sudah menjadi anggota menutup undangan tanpa mengubah rolenya. Verifikasi email diwajibkan saat menerima karena penerimaan memberi keanggotaan kepada pemilik alamat itu; tanpa itu orang yang mendaftar dengan email korban (belum terverifikasi) dan kebetulan memegang tautan yang diteruskan bisa menyamar.

- **Registrasi dengan email yang diundang.** Bila alamat yang mendaftar punya undangan yang masih berlaku (terbuka dan belum kedaluwarsa, organisasinya belum dihapus), `organization_name` diabaikan dan **tidak** dibuat organisasi baru. Keanggotaan sendiri diberikan saat alamat itu **terbukti**, yaitu dalam transaksi verifikasi email (`POST /auth/verify-email`) atau konfirmasi ganti email (`POST /me/email/confirm`): semua undangan berlaku untuk alamat itu diterima sekaligus (role sesuai undangan, yang sudah jadi anggota tetap pada rolenya). Ini sedikit lebih ketat daripada kalimat §7.4 ("setelah daftar ... otomatis diterima"): bila keanggotaan diberikan saat mendaftar, siapa pun bisa mendaftar dengan alamat korban yang belum terverifikasi dan masuk ke organisasi yang mengundangnya. Undangan yang kedaluwarsa, dicabut, atau ditolak tidak menghalangi pembuatan organisasi sendiri. Pencarian lintas tenant di sini memakai `db.WithBypass` di dalam transaksi registrasi/verifikasi.

- **Hapus akun dan organisasi.** `org.Service.BeforeUserDeleted` berjalan dalam transaksi penghapusan akun (dengan `db.WithBypass`, karena lintas tenant): owner organisasi yang masih punya anggota lain → `409` (`owns_shared_organization`) dan seluruh penghapusan dibatalkan; organisasi tempat user satu-satunya anggota di-soft-delete (undangan terbukanya dicabut; baris keanggotaan owner dipertahankan karena foreign key `owner_id` menunjuknya); keanggotaan di organisasi lain dihapus. Tiap baris organisasi dikunci `FOR UPDATE` sebelum anggotanya dihitung, sementara penerimaan undangan dan verifikasi email mengunci baris yang sama `FOR SHARE`, sehingga tidak ada yang bisa bergabung di antara pengecekan dan pembersihan (yang kalah balapan mendapati organisasi sudah terhapus). Organisasi yang ikut terhapus juga membatalkan subscription-nya dan me-`void` invoice terbukanya lewat hook yang sama dengan `DELETE /orgs/{orgId}`, di dalam transaksi penghapusan akun.

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

### 8.7 Keputusan implementasi

Hal-hal yang tidak ditentukan §8.1–8.6 dan sudah diputuskan di Fase 6:

- **Provider pembayaran.** Kontrak `PaymentProvider` (§8.1) ada di paket `internal/billing/payment` sebagai `payment.Provider`, dengan metode tambahan `Name()` dan tipe yang dinormalisasi (`PaymentRequest`, `PaymentSession`, `WebhookEvent`, `PaymentStatus`, `State`: `pending`, `paid`, `failed`, `expired`, `canceled`, `refunded`). `CreatePayment` menerima nomor invoice, nominal, mata uang, pelanggan, dan waktu kedaluwarsa tautan; `VerifyWebhook(r)` membaca body sendiri (sekali), memeriksa signature lebih dulu (`ErrInvalidSignature`) dan mengembalikan `Raw` (JSON) di `WebhookEvent`; `GetPaymentStatus(ref)` memakai referensi invoice dan mengembalikan `ErrNotFound` bila gateway tidak mengenalnya. Implementasi: `payment/midtrans` (produksi) dan `payment/fake` (dev/test; ditolak konfigurasi saat `app.env=production`). Pilihan lewat `billing.provider`; webhook diterima untuk semua provider yang terdaftar.
- **Midtrans: BELUM diuji ke sandbox sungguhan.** Akun sandbox belum ada (tugas manual pemilik repo, `docs/todo.md` Fase 4), jadi implementasi ditulis terhadap dokumentasi publik Midtrans dan diuji dengan server HTTP palsu (`httptest`) serta vektor signature yang dihitung di luar kode. Asumsi yang harus diverifikasi saat akun ada: (1) `POST {snap}/snap/v1/transactions` dengan Basic auth `ServerKey:` dan body `transaction_details` / `customer_details` / `item_details` / `expiry` menjawab `201` berisi `token` dan `redirect_url`; (2) `expiry` menerima `start_time` berformat `2006-01-02 15:04:05 -0700`, `unit: "minutes"`, dan durasi sepanjang masa trial (14 hari = 20160 menit) tanpa batas atas; (3) notifikasi memuat `transaction_id`, `transaction_status`, `status_code`, `order_id`, `gross_amount` (string `"99000.00"`), `fraud_status`, `signature_key`, dan `currency`; signature = hex(SHA512(`order_id` + `status_code` + `gross_amount` + ServerKey)) atas teks persis seperti dikirim; (4) `GET {api}/v2/{order_id}/status` memakai Basic auth yang sama, menjawab bentuk yang sama dengan notifikasi, dan menjawab `status_code: "404"` (HTTP 200 atau 404) untuk order yang belum pernah dibayar; (5) notifikasi Midtrans dikirim ulang bila tidak dijawab 2xx; (6) `settlement` selalu menyusul `capture` untuk kartu. Midtrans yang dituju diatur `billing.midtrans.*`; Server Key hanya di `config.json`, tidak pernah masuk log maupun pesan error.
- **ID event Midtrans.** Notifikasi Midtrans tidak punya ID unik, jadi ID diturunkan: `transaction_id:transaction_status:status_code:fraud_status` (`order_id` bila tidak ada `transaction_id`). Pengiriman ulang notifikasi yang sama menghasilkan ID yang sama (idempotensi §8.5), sedangkan `capture` lalu `settlement` untuk transaksi yang sama adalah dua event berbeda yang keduanya aman diproses. Status yang tidak dikenal dipetakan ke `pending` (tidak ada yang diaktifkan; payload tetap tersimpan di `payment_events`); `gross_amount` dengan pecahan selain nol ditolak, bukan dibulatkan.
- **Nomor invoice.** `INV-<tahun>-<sequence 6 digit>` dari sequence PostgreSQL `invoice_number_seq` (global, §14 #5). Sequence tidak mulai ulang tiap tahun (tahun hanya hiasan untuk pembaca; nomornya tetap unik tanpa tahun) dan tidak ikut di-rollback: checkout yang gagal meninggalkan celah dalam penomoran, yang disengaja. Lewat sejuta, digit bertambah. Role aplikasi memakai sequence lewat default privileges migrasi `00007` (diuji).
- **State machine (fungsi murni).** `billing.Next(snapshot, event, now) (Result, error)` di `internal/billing/statemachine.go` tidak membaca clock maupun database; kode yang menyimpan hasilnya (jalur webhook dan job) hanya menerapkan yang dikatakannya. Tabel transisi (`transitions`) adalah satu-satunya yang menentukan pasangan status-event yang sah; selain itu `ErrIllegalTransition`. Event: `trial_started` (Free → trialing), `invoice_paid`, `trial_ended`, `period_ended`, `grace_ended`, `organization_deleted`. Aturan yang tidak tercakup diagram §8.2:
  - **Trial** hanya untuk organisasi yang masih Free (tanpa `current_period_end`) dan belum pernah trial (`trial_ends_at` kosong; kolom itu tetap terisi selamanya sebagai penanda "trial sudah dipakai"). Periode trial = trial itu sendiri (`current_period_end = trial_ends_at`).
  - **Invoice `checkout` yang dibayar** (dari status mana pun, termasuk `expired`/`canceled`: berlangganan lagi) mengaktifkan plan invoice dengan periode baru **mulai saat dibayar** (§8.4); sisa periode lama tidak dikreditkan, dan `cancel_at_period_end`, `pending_plan_id`, `grace_until` dibersihkan. Membayar di tengah trial berarti sisa hari trial hangus.
  - **Invoice `renewal` yang dibayar** hanya berarti bagi subscription `active`/`past_due` untuk periode tepat setelah periode berjalan (`period_start` = `current_period_end`); selain itu `ErrStaleInvoice` (uang masuk, perlu dilihat manusia). Saat masih `active` pembayaran hanya dicatat (`paid_ahead`): periode baru, termasuk downgrade terjadwal, baru berlaku ketika periode lama habis (`period_ended`), supaya downgrade tidak berlaku lebih awal. Saat `past_due` langsung berlaku dan periode baru menyambung dari akhir periode lama (tanpa celah dan tanpa hari gratis).
  - **`period_ended`** (hanya pada/setelah `current_period_end`; subscription Free tidak pernah memenuhinya) diputuskan berurutan: `cancel_at_period_end` → `canceled` (walau periode berikutnya sudah dibayar: tidak ada refund di Tier 1); renewal berikutnya sudah dibayar → `active` periode baru (plan invoice; `pending_plan_id` yang sama dengan plan itu dihabiskan); selain itu `past_due` dengan `grace_until` = akhir periode + `grace_days`. Grace dihitung dari akhir periode, bukan dari kapan job sempat berjalan.
  - **Idempoten:** event berbasis waktu yang diterapkan lagi pada hasilnya ditolak (status baru tidak menerimanya), jadi job yang jalan dua kali tidak mengubah apa pun. `DueEvent` memilih event waktu yang jatuh tempo untuk sebuah snapshot.
  - Periode `month`/`year` dihitung dengan `AddInterval`: tanggal yang tidak ada di bulan tujuan menjadi hari terakhir bulan itu (31 Jan + 1 bulan = 28/29 Feb), bukan meluber ke bulan berikutnya.
- **Pembagian modul dan otorisasi.** `billing` mengimpor `org` hanya untuk `org.Authorize`/`org.Require` (`ActionManageBilling`, §7.2); `org` tidak mengimpor `billing` dan mendeklarasikan hook yang dibutuhkannya sendiri (§7.6). Kebalikannya, `billing` menulis surat ke pemilik organisasi lewat `OrganizationContacts` (diimplementasikan `org.Service.BillingContact`, termasuk organisasi yang sudah dihapus karena pembayaran terlambat tetap perlu dilaporkan), jadi tidak pernah membaca tabel `organizations`/`users`. `billing.Service` dibangun tanpa sisi pembayaran (cukup untuk `StartFree` dan membaca langganan) dan baru diaktifkan `EnablePayments(...)` di composition root setelah modul-modul yang saling membutuhkan itu ada. Checkout, change-plan, cancel, resume, dan `GET /invoices` hanya untuk **owner**: dicek di route (`org.Require`, 403 cepat) dan lagi di service (melindungi pemanggil non-HTTP). Checkout dan change-plan juga mewajibkan **email terverifikasi** (`EMAIL_NOT_VERIFIED`) karena membuat pembayaran dan mengirim email. Semua rute baru ada di inventaris `TestTenantIsolation_TheOrganizationRouteInventory`, sehingga non-anggota otomatis diuji mendapat `404`.
- **Checkout (`POST /orgs/{orgId}/subscription/checkout`, body `{"plan": "<kode>"}`).** Klien hanya menyebut kode plan; nominal, mata uang, dan panjang periode selalu dari tabel `plans` (field lain ditolak `400` sebagai field tak dikenal) dan disalin ke invoice, jadi perubahan harga kemudian tidak menyentuh invoice yang sudah terbit. Plan harus ada, aktif, dan berharga > 0 (`VALIDATION_ERROR` pada `plan`). Respons `{"invoice", "subscription", "trial_started"}`: `201` untuk invoice baru, `200` bila yang dikembalikan adalah invoice yang masih bisa dibayar untuk plan yang sama (idempoten: dipanggil dua kali, atau klik ganda, tetap satu invoice, satu pembayaran di gateway, satu email). Aturan:
  - **Trial.** Organisasi yang masih Free dan belum pernah trial mendapat trial plan yang punya `trial_days > 0`: subscription langsung `trialing` pada plan itu (`trial_ends_at = sekarang + trial_days`), tanpa pembayaran. Invoice pertamanya jatuh tempo saat trial berakhir dan bisa dibayar lebih awal (`trialing → active`; periode berbayar mulai saat itu, sisa hari trial hangus). Trial yang tidak dibayar menjadi `expired` oleh `billing.expire`. `trial_ends_at` tetap terisi sebagai penanda trial sudah dipakai, jadi checkout berikutnya hanya membuat invoice biasa (jatuh tempo `invoice_due_days`) dan plan baru aktif setelah dibayar.
  - **Plan sama sudah aktif** (`active` dengan plan itu): `409` (`details.reason = "already_subscribed"`). Invoice renewal yang sudah terbit untuk plan itu dikembalikan (membayar renewal lebih awal boleh).
  - **Plan lain**: checkout yang masih menunggu bayar untuk plan lain di-`void` (satu checkout hidup per subscription, dijaga indeks unik parsial); tautan gateway lamanya tidak bisa dibatalkan lewat kontrak `PaymentProvider`, jadi pembayaran yang masuk ke invoice `void` hanya disimpan dan ditandai untuk ditinjau (§8.5). Tautan yang sudah kedaluwarsa tidak dikembalikan lagi.
  - Organisasi `expired`/`canceled` boleh berlangganan lagi lewat checkout (tanpa trial; yang pernah berlangganan tidak lagi memenuhi syarat).
- **Gateway dipanggil di dalam transaksi.** Nomor invoice diambil, `CreatePayment` dipanggil, invoice dimasukkan `open`, dan email `invoice_created` diantre, semuanya dalam satu transaksi dengan baris subscription terkunci (`FOR UPDATE`), sehingga checkout bersamaan satu organisasi berjalan berurutan. Bila gateway gagal atau langkah mana pun gagal, tidak ada yang tersimpan (trial tidak mulai, tidak ada invoice, tidak ada email) dan klien menerima `502 PAYMENT_PROVIDER_ERROR`; sisanya hanya celah dalam penomoran. Konsekuensi yang diterima: satu koneksi database tertahan selama panggilan gateway (dibatasi `billing.midtrans.request_timeout`), dan bila commit gagal setelah gateway berhasil, tautan yatim tidak pernah dibagikan. Status `draft` disediakan skema tetapi tidak pernah disimpan.
- **Change plan, cancel, resume (§8.3-8.4).** Semuanya owner saja; `change-plan` juga butuh email terverifikasi. Respons `change-plan`: `{"subscription", "invoice", "scheduled", "cleared", "trial_started", "warnings"}`.
  - **Upgrade** (plan lebih mahal, atau plan berbayar mana pun dari Free): sama dengan checkout; invoice (`201`) dan plan berganti, dengan periode baru mulai saat itu, hanya setelah dibayar.
  - **Downgrade** (plan lebih murah atau sama harga, dari subscription `active` berbayar; `free` tidak bisa dipilih, pakai `cancel`): tidak ada yang dibayar dan tidak ada yang berubah sekarang. Plan dicatat di `pending_plan_id` (`scheduled: true`, `200`), renewal untuk plan lama yang sudah terbit di-`void` dan job renewals menerbitkannya lagi untuk plan baru, sehingga plan baru berlaku ketika periode berakhir lewat pembayaran renewal itu. `warnings` memuat tiap limit yang pemakaiannya melebihi plan baru (`limit`, `used`, `max`; limit yang tidak tercantum di plan baru dihitung 0, seperti di entitlement). Data tidak dihapus; hanya pembuatan baru yang akan diblokir setelah plan baru berlaku (§8.4). Memesan downgrade yang sama dua kali tidak mengubah apa pun. Bila renewal periode berikutnya sudah dibayar sebelum downgrade dipesan, plan baru berlaku satu periode kemudian.
  - Meminta plan yang sedang dipakai membatalkan downgrade terjadwal (`cleared: true`); tanpa downgrade terjadwal itu `409` (`already_subscribed`).
  - **Trial atau `past_due`** tidak punya periode untuk ditunggu: plan lain dibeli lewat invoice seperti checkout, tanpa penjadwalan. **`expired`/`canceled`**: `409` (`use_checkout`).
  - **Cancel** (`200`): `cancel_at_period_end = true`, akses berlanjut sampai periode habis, tidak ada refund; renewal yang masih menunggu bayar di-`void` supaya tidak ada pembayaran yang memperpanjang langganan yang akan berakhir. Idempoten. **Resume** (`200`) membatalkannya; renewal diterbitkan lagi oleh job saat waktunya. Free/`expired`/`canceled` → `409` (`nothing_to_cancel` / `nothing_to_resume`). Aturan ini juga fungsi murni (`RequestCancellation`, `RevokeCancellation`, `ScheduleDowngrade`) yang diuji tabel.
- **Organisasi dihapus.** `org.deleteOrganization` (dipakai `DELETE /orgs/{orgId}` dan guard penghapusan akun) memanggil `SubscriptionLifecycle.OrganizationDeleted` di transaksi yang sama (interface dideklarasikan `org`, diimplementasikan `billing.Service`): subscription menjadi `canceled` dari status mana pun (event `organization_deleted`; `pending_plan_id`, `grace_until`, `cancel_at_period_end` dibersihkan) dan semua invoice `draft`/`open` di-`void`; invoice `paid` adalah riwayat dan tidak disentuh. Idempoten, dan organisasi tanpa subscription tidak masalah. Tautan gateway invoice yang di-`void` tidak bisa dicabut lewat kontrak `PaymentProvider`, jadi pembayaran yang masuk kemudian hanya dicatat dan ditandai untuk ditinjau manusia (§8.5), tidak pernah mengaktifkan organisasi yang sudah dihapus.
- **Invoice.** `GET /orgs/{orgId}/invoices` (berpaginasi cursor, terbaru dulu) dan `GET .../invoices/{id}` (invoice organisasi lain `404`); `payment_url` hanya berisi tautan selama invoice `open`. Tanggal di email ditulis dalam WIB dan bahasa Indonesia, nominal IDR seperti `Rp99.000`.
- **Pengingat tidak dobel.** Invoice yang dibuat di dalam jendela H-3 (atau H-1) sebelum jatuh temponya menandai pengingat itu sebagai terkirim sejak awal: email `invoice_created` sudah menjadi pemberitahuannya.
- **Webhook (`POST /api/v1/webhooks/{provider}`).** Publik: signature adalah kredensialnya, diperiksa oleh kode gateway itu sendiri (`VerifyWebhook`) sebelum apa pun; tidak lolos → `401 UNAUTHENTICATED` dan tidak ada yang disimpan, body tak terbaca → `400`, provider tak terdaftar → `404`. Body dibatasi 1 MiB. Event yang lolos disimpan ke `payment_events` dengan `ON CONFLICT (provider, event_id) DO NOTHING`, sehingga pengiriman ulang gateway (yang terjadi tiap kali ia tidak menerima 2xx) menjadi satu event dan dijawab `200` supaya gateway berhenti mencoba. Webhook tidak punya tenant sebelum dicocokkan dengan invoice, jadi berjalan di transaksi sistem (`WithSystemTx`) dengan filter eksplisit.
- **Pemrosesan webhook (§8.5 langkah 3-4).** Dua transaksi: (1) event disimpan dan di-commit apa pun yang terjadi sesudahnya; (2) baris event dikunci (`FOR UPDATE`; pengiriman ganda, termasuk yang bersamaan, atau retry menunggu giliran dan melihat `processed_at`), lalu pembayaran diterapkan: kunci subscription, lalu invoice (urutan **subscription → invoice** dipakai semua penulis, jadi webhook dan job tidak saling deadlock), invoice ditandai `paid`, state machine dijalankan, job email diantre, `processed_at` diisi. Gagal di mana pun → rollback semuanya, `error` disimpan pada event (di transaksi terpisah), `processed_at` tetap kosong, dan respons `500` supaya gateway mengirim ulang; kiriman ulang memproses event yang sama (tidak membuat baris baru) dan, bila berhasil, mengosongkan `error`. Hasil bisnis yang bukan kegagalan (retry tidak akan memperbaikinya) diproses dengan `200`, dan catatannya disimpan di kolom `error` berdampingan dengan `processed_at` serta di-log level error ("needs manual attention"):
  - **Nominal atau mata uang tidak sama dengan invoice** → invoice tetap `open`, tidak ada yang aktif. Nominal selalu dari invoice yang kita terbitkan, tidak pernah dari notifikasi.
  - **Pembayaran untuk invoice `void`/`expired`** (mis. checkout yang digantikan, atau yang jatuh tempo) → tidak diaktifkan diam-diam; uangnya perlu dikembalikan atau langganan dibuka manual.
  - **Invoice tidak dikenal**, atau milik provider lain → dicatat saja.
  - **Pembayaran yang tidak lagi cocok dengan subscription** (renewal dibayar setelah subscription `expired`/`canceled`) → invoice tetap ditandai `paid` (uangnya nyata) tetapi subscription tidak berubah dan event ditandai.
  - **Refund/chargeback** → hanya ditandai (di luar Tier 1).
  - Event yang datang terlambat atau berulang tidak pernah memundurkan invoice: `expired`/`cancel`/`pending`/`failed` setelah `paid` tidak mengubah apa pun; `paid` kedua (mis. `capture` lalu `settlement`) tidak mengubah apa pun dan tidak mengirim email lagi. `expire` menutup invoice `open` menjadi `expired`, `cancel` menjadi `void`; `pending`/`failed` membiarkannya `open` (pelanggan boleh mencoba lagi).
  - Saat invoice `checkout` dibayar, periodenya ditulis ulang ke periode yang benar-benar dibayar (mulai saat dibayar) dan invoice hidup lain milik subscription itu (mis. renewal untuk periode lama) di-`void`. Email `payment_succeeded` dikirim ke pemilik organisasi, juga bila organisasinya sudah dihapus.
- **Scheduler (`internal/platform/scheduler`, dijalankan `cmd/worker`).** Satu goroutine (lewat `safego`, berhenti bersama `ctx` worker) menjalankan job satu per satu sesuai urutan registrasi: tiap job jalan saat start dan lagi setelah intervalnya (`billing.hourly_jobs_interval` untuk `renewals`, `expire`, `apply_scheduled`; `billing.daily_jobs_interval` untuk `reminders`, `reconcile`). Satu goroutine berarti satu koneksi tambahan saja (kolam koneksi tidak kelaparan) dan job yang lambat hanya menunda job lain, tidak menumpuk. Sebelum menjalankan sebuah job, scheduler mengambil **advisory lock transaksi** (`pg_try_advisory_xact_lock`, kunci = hash nama job) dalam transaksi diam yang dipegang selama job berjalan: worker lain yang melihat lock terpegang melewatkan job itu (bukan error); lock lepas sendiri saat transaksi berakhir atau koneksi putus (worker mati), jadi tidak ada `unlock` yang bisa terlupa. **Lock hanya mencegah kerja ganda, bukan syarat kebenaran**: setiap job idempoten dan aman dijalankan bersamaan dengan sendirinya (baris dikunci `FOR UPDATE SKIP LOCKED` dan kondisinya dicek ulang setelah terkunci), dan diuji dengan menjalankan job yang sama dari dua goroutine tanpa lock. Satu run punya `trace_id` baru; hasilnya dilog sebagai `job finished` dengan nama job, jumlah per kategori, lama, dan error bila ada; panic menjadi error dan lock tetap dilepas. Batas waktu satu run 10 menit. Waktu dibaca dari `platform/clock`, dan `RunDue`/`RunOnce` bisa dipanggil langsung oleh test tanpa tidur.
- **Pola semua job (§8.6).** Tiap job adalah method `Service` yang hanya menerima `ctx` (waktu dari `platform/clock`) dan mengembalikan hitungan per kategori (`issued`, `exists`, `skipped`, `failed`, ...), didaftarkan lewat `Service.RegisterJobs`. Kandidat dibaca per halaman (keyset pada `id`, 100 baris), lalu **tiap baris diproses di transaksi sistem sendiri** dengan `FOR UPDATE SKIP LOCKED` pada baris subscription/invoice dan kondisinya **dicek ulang setelah terkunci**: baris yang sedang dipegang worker lain dilewati (`busy`), baris yang sudah ditangani tidak lagi cocok. Yang dibuat job dijaga indeks unik (satu renewal per periode) atau ditandai di transaksi yang sama dengan pengiriman email (pengingat), sehingga menjalankan ulang atau bersamaan tidak membuat apa pun dua kali. Satu baris yang gagal (mis. gateway turun) dilog, dihitung `failed`, dan dicoba lagi pada run berikutnya tanpa menghentikan baris lain; error gabungannya dikembalikan di akhir supaya scheduler mencatat run itu sebagai gagal.
- **`billing.renewals`.** Untuk subscription `active`/`past_due` berbayar yang tidak diset berhenti dan periodenya habis dalam `renewal_lead_days` (atau sudah habis, bila worker sempat mati), menerbitkan invoice `renewal` periode berikutnya: plan = `pending_plan_id` bila ada (downgrade berlaku lewat renewal-nya), periode mulai saat periode berjalan berakhir, jatuh tempo saat itu juga, tautan bayar berlaku sampai jatuh tempo + `grace_days`. Tanpa renewal bagi: Free, trial, `expired`/`canceled`, yang `cancel_at_period_end`, organisasi yang sudah dihapus, dan renewal yang tautannya sudah mati (`too_late`; langganan itu kembali lewat checkout). Menjalankan ulang tidak membuat apa pun (`exists`), dan renewal yang di-`void` (cancel lalu resume, atau downgrade dipesan) diterbitkan lagi karena indeks uniknya mengabaikan yang `void`. Email `invoice_created` dikirim bersama invoicenya.
- **`billing.expire` dan `billing.apply_scheduled` (§8.6).** Keduanya membaca daftar subscription yang event waktunya jatuh tempo (`DueEvent`: trial habis, periode habis, grace habis) dan menyerahkan keputusan ke `Next`, yang memilih berdasarkan prioritas tetap; tiap job hanya menjalankan hasil miliknya, sehingga satu baris tidak pernah ditangani keduanya dan urutan jalannya tidak berpengaruh:
  - `billing.expire`: `trialing → expired` (trial tak dibayar; email `subscription_expired`), `active → past_due` (periode habis tanpa renewal terbayar dan tanpa pembatalan; `grace_until` = akhir periode + `grace_days`, dihitung dari akhir periode, bukan dari kapan job sempat jalan; email `subscription_past_due` berisi tautan bayar renewal bila ada, bila tidak mengarah ke pengaturan langganan), `past_due → expired` (grace habis; renewal terbuka di-`expire`; email `subscription_expired`). Lalu menyapu invoice yang tak mungkin dibayar lagi menjadi `expired`: checkout lewat `due_at`, renewal lewat `due_at` + grace (waktu yang sama dengan matinya tautan gateway). Satu langkah per baris per run; worker yang lama mati mengejar satu langkah per run.
  - `billing.apply_scheduled`: periode `active` yang sudah habis dan (a) `cancel_at_period_end` → `canceled` (menang atas renewal berikutnya yang sudah dibayar: tidak ada refund di Tier 1; renewal terbuka di-`void`); (b) renewal berikutnya sudah dibayar → bergulir ke periode itu dengan plan renewal tersebut, sehingga downgrade yang dipesan di `pending_plan_id` berlaku di sini lewat renewal-nya.
  - **Akurasi akses.** Akhir masa tenggang tepat waktu walau job terlambat (entitlement membandingkan `grace_until` dengan clock). Yang menunggu job adalah perpindahan `active → past_due`/`expired`: akses bisa berjalan paling lama satu interval job (default 1 jam) melewati akhir periode atau trial, atau lebih lama bila worker mati.
- **`billing.reminders` (harian).** Invoice `open` yang belum jatuh tempo diingatkan sekali saat tersisa 3 hari dan sekali saat tersisa 1 hari: `trial_ending` untuk invoice pertama trial (jatuh temponya adalah akhir trial), `payment_reminder` untuk yang lain, berisi `days_left` (dibulatkan ke atas) dan tautan bayar. Penanda `reminder_3d_sent_at`/`reminder_1d_sent_at` ditulis di transaksi yang sama dengan antrean emailnya (gagal antre → tidak ditandai → dicoba lagi), jadi run ulang atau dua worker tidak mengirim dua kali. Invoice yang lahir di dalam jendela sudah ditandai (emailnya `invoice_created` adalah pemberitahuannya); worker yang lama mati dan menemukan invoice di dalam jendela 1 hari mengirim satu email untuk kedua jendela. Tidak ada pengingat untuk invoice yang sudah lunas/ditutup/lewat jatuh tempo atau subscription yang tidak berjalan lagi. Karena job harian, waktu pengingat bisa terlambat sampai satu hari dari batas jendela.
- **`billing.reconcile` (harian).** Untuk tiap invoice `open` yang lebih tua dari `reconcile_min_age` (default 1 jam; pelanggan mungkin masih di halaman bayar), menanyakan status ke gateway (`GetPaymentStatus` dengan nomor invoice) dan, bila jawabannya `paid`/`expired`/`canceled`, memasukkannya lewat **jalur webhook yang sama** sebagai event dengan ID turunan `reconcile:<nomor>:<state>`: disimpan di `payment_events`, dikunci dan diproses dengan aturan yang sama (termasuk pemeriksaan nominal/mata uang terhadap invoice), sehingga memperbaiki webhook yang hilang atau yang sudah menyerah dikirim ulang gateway, dan aman dijalankan ulang, bersamaan dengan webhook asli yang terlambat, atau di dua worker. `pending`/`failed` dan pembayaran yang tak dikenal gateway (`ErrNotFound`: pelanggan belum pernah memulai) dibiarkan. Invoice milik provider yang tidak terdaftar di deployment ini dilewati.
- **Yang sengaja tidak dikerjakan (§13 dan sekitarnya).** Proration, kupon, pajak, multi-currency, refund otomatis (refund/chargeback hanya ditandai untuk ditinjau manusia), pembatalan tautan gateway (kontrak `PaymentProvider` tidak punya `Cancel`; invoice yang di-`void` atau digantikan hanya tidak lagi mengaktifkan apa pun bila dibayar), notifikasi in-app, Xendit, dan retry/backoff khusus untuk panggilan keluar ke gateway selain timeout klien. Status invoice `draft` ada di skema tetapi tidak pernah disimpan (checkout dan job menerbitkan invoice dalam satu transaksi dengan pembayarannya).
- **Konfigurasi.** Section `billing` di `config.json`: `provider` (`fake` default; `midtrans` wajib `midtrans.server_key`), `invoice_due_days` (7), `renewal_lead_days` (3), `grace_days` (3), `hourly_jobs_interval` (1h) dan `daily_jobs_interval` (24h) untuk scheduler worker, `reconcile_min_age` (1h), dan `midtrans.{server_key, snap_base_url, api_base_url, request_timeout}` (default sandbox). Di production: `fake`, host sandbox, kunci berawalan `SB-`, dan placeholder ditolak saat start.

---

## 9. Modul Entitlement (Feature Gating & Limit)

### 9.1 Interface

```go
type Entitlements interface {
    HasFeature(ctx context.Context, q db.DBTX, orgID uuid.UUID, feature string) (bool, error)
    CheckLimit(ctx context.Context, q db.DBTX, orgID uuid.UUID, limit string, delta int) error // PLAN_LIMIT_REACHED jika melebihi
    Usage(ctx context.Context, q db.DBTX, orgID uuid.UUID) (map[string]LimitUsage, error)
}

type LimitUsage struct{ Used, Max int } // Max -1 = tak terbatas
```

`q` adalah handle DB pemanggil (transaksi yang terpaku ke tenant), supaya pengecekan ikut transaksi pembuatan resource (lihat §9.4).

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

### 9.4 Keputusan implementasi

Hal-hal yang tidak ditentukan §9.1–9.3 dan sudah diputuskan di Fase 5:

- **Pembagian modul.** `internal/billing` memiliki `plans` dan `subscriptions` (nanti invoice dan provider pembayaran di Fase 6) beserta paket sqlc `billingdb`. `internal/entitlement` hanya lapisan kebijakan tipis di atasnya. Modul `org` tidak mengimpor `billing` dan sebaliknya; `org` mendeklarasikan `SubscriptionStarter` yang dipenuhi `billing.Service`.
- **Subscription Free.** `org.CreateWithOwner` (jalur registrasi dan `POST /orgs`) memanggil `StartFree` tepat setelah baris owner dimasukkan, di transaksi yang sama dan sudah terpaku ke tenant baru: organisasi tidak pernah ada tanpa subscription-nya, dan bila katalog tidak punya plan `free` seluruh pembuatan organisasi dibatalkan. Subscription Free berstatus `active`, `current_period_end` `NULL` (§4.8), `current_period_start` = waktu pembuatan.
- **Plan efektif.** `billing.Service.EffectivePlan` mengembalikan plan subscription bila `GrantsAccess(status, grace_until, now)` benar, selain itu plan `free`; organisasi tanpa baris subscription juga `free`. `GrantsAccess` adalah fungsi murni (waktu dari `platform/clock`): `trialing` dan `active` ya; `past_due` hanya selama `now < grace_until` (tepat di `grace_until` sudah tidak); `expired`, `canceled`, status tak dikenal tidak (fail closed). Plan Free yang hilang dari katalog adalah error, bukan akses tanpa batas. `billing.Service` memenuhi `entitlement.PlanSource` (`EffectivePlan`, `ActivePlans`); `entitlement` mendefinisikan tipe `Plan` dan tidak mengimpor `billing` maupun `org`.

---

## 10. Modul Email Transaksional

### 10.1 Arsitektur

```
service (dalam transaksi) ──insert──▶ outbox_jobs ──▶ worker ──▶ Mailer (SMTP / API provider)
```

- Interface `Mailer` supaya provider bisa diganti; implementasi dev menulis ke log atau Mailpit.
- Retry dengan exponential backoff: 1m, 5m, 30m, 2j, 6j. Artinya satu percobaan awal ditambah 5 retry; bila retry ke-5 juga gagal → `failed` dan dicatat untuk dicek manual (total 6 percobaan). Error yang ditandai permanen (mis. template tidak dikenal) langsung `failed` tanpa retry.
- Pengiriman bersifat *at-least-once*: bila worker mati setelah email terkirim tetapi sebelum job ditandai `done`, job diambil ulang dan email bisa terkirim dua kali. Ini disengaja; alternatifnya (at-most-once) bisa menghilangkan email.
- Outbox adalah mekanisme generik (`platform/outbox`): worker memilih handler berdasarkan `type` job; email hanya salah satu tipenya (`email.send`).
- Template HTML ditulis dengan `html/template` (auto-escape); versi plain text dengan `text/template` karena `html/template` akan meng-escape teks biasa.

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

- **Keamanan:** HTTPS wajib, header keamanan standar, CORS terbatas, semua input divalidasi, query parametrized, secret (password DB, SMTP, kunci gateway) hanya di `config.json` yang tidak masuk git (`.gitignore`); repo hanya berisi `config.example.json` tanpa secret nyata.
- **Observability:** structured logging (JSON) dengan `trace_id`, `request_id`, `user_id`, `org_id` (lihat §11.1); endpoint `/healthz` dan `/readyz`.
- **Konsistensi:** semua operasi multi-tabel dalam satu transaksi.
- **Testing:** unit test untuk service layer; integration test dengan PostgreSQL asli (testcontainers) untuk repository, webhook, dan isolasi tenant.
- **Seed data:** perintah untuk membuat plan default dan akun demo di environment dev.

### 11.1 Logging, trace ID & context propagation

Tujuan: satu `trace_id` bisa dipakai untuk menelusuri request HTTP sampai job outbox yang dihasilkannya (mis. email verifikasi), lintas modul dan proses.

- **Dua ID.** `request_id` unik per request HTTP; `trace_id` mengikuti alur kerja end-to-end (request → outbox → worker). Format `trace_id`: 32 karakter hex (kompatibel W3C Trace Context).
- **Asal ID.** Middleware HTTP membaca header `traceparent` (W3C) bila ada dan valid; selain itu membuat `trace_id` baru. `request_id` selalu dibuat baru. Keduanya dikembalikan lewat header `X-Request-ID` dan `X-Trace-ID`.
- **Context propagation.** `trace_id`, `request_id`, `user_id`, `org_id` disimpan di `context.Context` lewat helper `platform` (key bertipe privat, bukan string). Semua fungsi yang melakukan I/O menerima `ctx context.Context` sebagai parameter pertama dan meneruskannya ke layer di bawahnya (service → repository → `pgx`, `PaymentProvider`, `Mailer`). ID tidak pernah dioper sebagai argumen terpisah atau dibaca dari variabel global.
- **Dependency injection.** Logger dibuat sekali di composition root (`cmd/api`, `cmd/worker`) dan **di-inject lewat constructor** ke middleware, service, repository, dan worker; tanpa logger global dan tanpa framework DI (wiring manual). Saat menulis log, komponen mengambil logger turunan dari context lewat method `FromContext(ctx)` pada logger yang di-inject (`log.FromContext(ctx)`) yang otomatis menyertakan field ID di atas, jadi pemanggil tidak perlu menulis field itu manual.
- **Lintas proses (outbox).** `Enqueue(tx, job)` menyimpan `trace_id` dari context ke kolom `outbox_jobs.trace_id`. Worker memulihkan `trace_id` ke context sebelum memproses job (dan membuat `request_id`/`job_id` baru), sehingga log pengirim email memakai `trace_id` yang sama dengan request asalnya.
- **Panggilan keluar.** Panggilan ke gateway pembayaran meneruskan `trace_id` lewat context (dan header `traceparent` bila didukung) agar bisa dicocokkan di log.
- **Format log.** JSON satu baris per event; field minimal: `time`, `level`, `message`, `trace_id`, `request_id`; ditambah `user_id`/`org_id` bila sudah diketahui (setelah auth/tenant middleware). Token, password, dan header `Authorization` tidak boleh masuk log.
- **Test.** Unit test memastikan logger dari context memuat field ID; integration test memastikan `trace_id` request sampai ke job outbox dan log worker.

### 11.2 Konkurensi (goroutine)

- **Pakai goroutine untuk pekerjaan yang independen dan bisa paralel** (mis. beberapa query baca yang tidak saling bergantung, panggilan ke beberapa layanan eksternal, pemrosesan job di worker). Jangan dipakai untuk urutan yang saling bergantung, dan jangan menambah konkurensi tanpa alasan.
- **Dilarang `go func() {...}()` mentah** di kode aplikasi. Semua goroutine dibuat lewat helper `platform/safego`, supaya panic tertangkap dan goroutine tidak lepas kontrol.
- **Helper `safego`:**
  - `Go(ctx, name, fn)` untuk fire-and-forget: `recover()` panic, catat log level error berisi `name`, panic value, stack trace, dan `trace_id` dari context; proses tidak crash. Goroutine ikut terlacak (WaitGroup) agar graceful shutdown bisa menunggunya.
  - `Group` (dibangun di atas `errgroup`) untuk fan-out/fan-in: `Go(name, fn func(ctx) error)`, `Wait() error`. Panic diubah menjadi error, error pertama membatalkan context anggota lain, dan ada batas konkurensi (`SetLimit`) agar tidak membuat goroutine tanpa batas.
  - Runner menerima logger lewat constructor (sesuai §11.1) dan meneruskan `ctx` ke fungsi yang dijalankan sehingga `trace_id` ikut.
- **Mencegah goroutine leak.** `recover` hanya mencegah crash; kebocoran dicegah dengan aturan berikut: setiap goroutine menerima `ctx` dan **wajib berhenti saat `ctx` dibatalkan**; setiap goroutine punya pemilik yang menunggunya (`Wait()` atau WaitGroup milik runner); channel tidak dibiarkan menggantung (selalu ada jalur keluar lewat `ctx.Done()`); server dan worker menunggu goroutine selesai (dengan timeout) saat shutdown.
- **Transaksi tidak boleh dipakai paralel.** `pgx.Tx` tidak aman untuk akses bersamaan, jadi operasi di dalam satu transaksi (`WithTx`) berjalan berurutan. Paralelisme hanya untuk operasi di luar transaksi atau yang memakai koneksi/transaksi sendiri.
- **Test.** Unit test `safego`: panic tertangkap dan dilog, error `Group` merambat, batas konkurensi dihormati, context batal menghentikan goroutine, tidak ada goroutine tersisa setelah `Wait()` (mis. dengan `goleak`).

---

## 12. Kriteria Selesai (Acceptance Criteria)

### Auth
- [x] User bisa daftar, otomatis punya organisasi dan plan Free — `TestRegister_EndToEnd`, `TestRegister_CreatesTheUsersOwnOrganizationAsOwner`, `TestRegister_GivesTheNewOrganizationAFreeSubscription`, `TestCreateOrg_GivesTheNewOrganizationAFreeSubscription` (`internal/app`), `TestCreateWithOwner_StartsAnActiveFreeSubscriptionInTheSameTransaction`, `TestCreateWithOwner_WithoutAFreePlanNothingIsCreated` (`internal/org`)
- [x] Verifikasi email, lupa password, dan reset password berjalan end-to-end — `TestVerifyEmail_EndToEnd`, `TestPasswordReset_EndToEnd` (`internal/app`)
- [x] Refresh token dirotasi; pemakaian ulang token lama mencabut seluruh family — `TestRefresh_ReusingARotatedTokenKillsTheWholeSession` (`internal/app`), `TestRotate_*` (`internal/auth`)
- [x] Rate limit login aktif — `TestLoginRateLimit_*` (`internal/app`)
- [x] `forgot-password` tidak membocorkan apakah email terdaftar — `TestForgotPassword_DoesNotRevealWhichEmailsAreRegistered`, `TestLogin_NoUserEnumeration` (`internal/app`)

### User
- [x] Ganti password mencabut sesi lain — `TestChangePassword_EndToEnd_KeepsThisSessionAndEndsTheOthers` (`internal/app`), `TestRevokeOtherSessions_*` (`internal/auth`)
- [x] Ganti email butuh konfirmasi dari email baru — `TestChangeEmail_EndToEnd_NothingChangesUntilTheNewInboxConfirms` (`internal/app`)
- [x] Hapus akun ditolak jika masih owner organisasi dengan anggota lain — `TestDeleteAccount_RealOrganizations_*` (`internal/app`), `TestGuard_*` (`internal/org`)

### Multi-tenancy & Tim
- [x] User di organisasi A tidak bisa membaca atau mengubah data organisasi B (ada test-nya) — `TestTenantIsolation_*` (`internal/app`, seluruh rute `/orgs/{orgId}` diambil dari router), `TestRLS_*` (`internal/org`, tingkat database)
- [x] Undangan bisa dikirim, diterima, ditolak, dibatalkan, dan kedaluwarsa — `TestInvite_*`, `TestAcceptInvitation_*`, `TestDeclineInvitation_*`, `TestRevokeInvitation_*`, `TestRegisterWithAnInvitedEmail_*` (`internal/app`)
- [x] Matriks izin §7.2 tercakup test — `TestCan_MatchesThePermissionMatrixCellByCell` (`internal/org`) dan `TestChangeRole_*`, `TestRemoveMember_*`, `TestInvite_FollowsThePermissionMatrix`, `TestUpdateOrg_*`, `TestDeleteOrg_*` (`internal/app`)
- [x] Transfer kepemilikan berjalan atomik — `TestTransferOwnership_IsAtomic`, `TestTransferOwnership_RacingTransfersCannotBothWin` (`internal/org`), `TestTransferOwnership_*` (`internal/app`)

### Billing
- [ ] Checkout menghasilkan payment link yang valid di sandbox gateway — **belum diverifikasi di sandbox Midtrans sungguhan** (akun belum ada; asumsi yang harus dicek di §8.7). Yang sudah teruji: `TestCheckout_EndToEnd_TheOwnerBuysAPlanAndGetsAnInvoiceWithAPaymentLink`, `TestCheckout_EndToEnd_*` (`internal/app`, fake provider), `TestCheckout_*` (`internal/billing`), dan sisi Midtrans terhadap server HTTP palsu dengan signature yang dihitung di luar kode: `TestCreatePayment_*`, `TestGetPaymentStatus_*`, `TestVerifyWebhook_*` (`internal/billing/payment/midtrans`)
- [x] Webhook yang sama dikirim dua kali hanya diproses sekali — `TestWebhook_TheSamePaymentDeliveredTwiceIsAppliedOnce`, `TestWebhook_DuplicateDeliveriesAtTheSameTimeAreAppliedOnce`, `TestWebhook_TheSecondNotificationOfOnePaymentUnderAnotherEventIDChangesNothing` (`internal/billing`), `TestWebhook_EndToEnd_ARedeliveryDoesNotPayTwice` (`internal/app`)
- [x] Webhook dengan signature salah ditolak — `TestWebhook_ASignatureThatDoesNotVerifyIsRejectedAndNothingIsStored` (`internal/billing`), `TestWebhook_EndToEnd_ASignatureThatDoesNotVerifyIs401AndStoresNothing` (`internal/app`), `TestVerifyWebhook_RejectsASignatureThatDoesNotMatch`, `TestVerifyWebhook_ASignatureOverTamperedFieldsFails` (Midtrans)
- [x] Transisi state machine §8.2 tercakup test (dengan clock yang bisa dimanipulasi) — `TestNext_*` (tabel transisi eksaustif status × event dengan `now` yang dipilih test, termasuk setiap transisi ilegal), `TestLifecycle_*` (setiap transisi §8.2 berurutan dengan fake clock lewat webhook dan job), `TestExpire_*`, `TestApplyScheduled_*`, `TestRenewals_*` (`internal/billing`)
- [x] Cron reconcile memperbaiki invoice yang webhook-nya hilang — `TestReconcile_RepairsAnInvoiceWhoseWebhookWasLost` (+ `TestReconcile_*`)
- [x] Job billing idempoten dan aman dijalankan bersamaan — `TestRenewals_RunningAgainCreatesNothing`, `TestRenewals_TwoWorkersAtTheSameTimeCreateOneInvoicePerSubscription`, `TestExpire_TwoWorkersAtTheSameTimeDoEachTransitionOnce`, `TestApplyScheduled_TwoWorkersAtTheSameTimeApplyEachOnce`, `TestReminders_TwoWorkersAtTheSameTimeSendEachOnce`, `TestReconcile_TwoWorkersAtTheSameTimeRepairEachInvoiceOnce`, `TestWorkers_TwoSchedulersRunningTheWholeScheduleAtOnceLeaveWhatOneWouldHaveLeft` (`internal/billing`), `TestPostgresLocker_*` (`internal/platform/scheduler`)
- [x] Organisasi yang dihapus membatalkan subscription-nya dan me-void invoice terbukanya, atomik — `TestDelete_CancelsTheSubscriptionAndVoidsItsOpenInvoices`, `TestGuard_AccountDeletionCancelsTheSubscriptionOfTheOrganizationItTakesWithIt`, `TestDelete_IfTheSubscriptionCannotEndTheOrganizationIsNotDeleted` (`internal/org`), `TestDeleteOrg_EndToEnd_*`, `TestDeleteAccount_EndToEnd_*` (`internal/app`)
- [x] Hanya owner dengan email terverifikasi yang bisa checkout/ganti plan; non-anggota tidak tahu organisasinya ada — `TestCheckout_EndToEnd_OnlyAVerifiedOwnerMayBuy`, `TestChangePlan_EndToEnd_*`, `TestCancelAndResume_EndToEnd_*`, `TestInvoices_EndToEnd_*`, `TestTenantIsolation_*` (`internal/app`)

### Entitlement
- [x] Menambah anggota melebihi limit menghasilkan `PLAN_LIMIT_REACHED` — `TestInvite_BeyondTheFreeLimitIsRefusedWithUpgradeDetails`, `TestInvite_MembersAndPendingInvitationsShareTheLimit`, `TestInvite_ParallelInvitationsAtTheLastSeatLetExactlyOneThrough`, `TestInvite_AnExpiredInvitationTakesASeatAgainWhenRefreshedOrResent` (`internal/app`), `TestCheckLimit_*` (`internal/entitlement`), `TestCountSeats_*` (`internal/org`)
- [x] Organisasi `expired` otomatis kembali ke limit Free — `TestInvite_ASubscriptionThatNoLongerGrantsAccessFallsBackToTheFreeLimit`, `TestInvite_PastDueKeepsThePlanOnlyDuringTheGracePeriod`, `TestRequireFeature_*` (`internal/app`), `TestEffectivePlan_*`, `TestGrantsAccess_*` (`internal/billing`)

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
- **Interface.** Pengikut konvensi repo, tiap method menerima handle DB (`db.DBTX`) supaya pengecekan ikut transaksi perubahan yang dijaganya; `q` harus transaksi yang terpaku ke tenant (atau transaksi sistem), bukan pool (RLS akan menampilkan nol baris). Bentuk akhir: `HasFeature(ctx, q, orgID, feature) (bool, error)`, `CheckLimit(ctx, q, orgID, limit, delta) error`, `Usage(ctx, q, orgID) (map[string]LimitUsage, error)` dengan `LimitUsage{Used, Max}` (menggantikan cuplikan §9.1). `entitlement.Service` juga punya `CheckFeature` (versi `HasFeature` yang mengembalikan error API) dan `RequireFeature` (§9.4 berikutnya).
- **Counter.** Pemakaian dihitung lewat `RegisterCounter(limit, fn)` yang dipasang composition root; modul pemilik resource menyediakan fungsinya (mis. `org.Service.CountSeats` untuk `members`). `Usage` hanya melaporkan limit yang punya counter (max `0` bila plan tidak mencantumkannya). Mendaftar nama ganda atau counter kosong adalah bug wiring (panic saat start).
- **`CheckLimit`.** Gagal dengan `PLAN_LIMIT_REACHED` bila `used + delta > max` (`max = -1` tak terbatas, tidak menghitung sama sekali). Limit yang tidak tercantum di plan efektif dianggap `0` (fail closed), bukan tak terbatas. Limit tanpa counter adalah error internal (bug pemrograman, bukan pesan untuk user), juga untuk `delta = 0`. `delta <= 0` tidak pernah ditolak, termasuk saat organisasi sudah melampaui limit setelah downgrade (§8.4: yang diblokir hanya penambahan). `CheckLimit` tidak mengunci apa pun; pemanggil yang bisa berebut (undangan) mengunci baris organisasi lebih dulu.
- **Detail error.** Respons `PLAN_LIMIT_REACHED` (HTTP 402) membawa `details`: `limit`, `max`, `used`, `plan` (kode plan saat ini), `upgrade_to` (kode plan aktif termurah yang muat `used + delta`, atau `null`). `PLAN_FEATURE_UNAVAILABLE` (403) membawa `feature`, `plan`, `upgrade_to` (plan aktif termurah yang punya fitur itu, atau `null`). Pesan berbahasa Indonesia, mis. "Paket Free hanya mendukung 3 anggota." (label limit di peta kecil `members` → "anggota") dan "Fitur ini tidak tersedia di paket Free.".
- **Limit `members` (kursi).** `org.Service.CountSeats` = jumlah `memberships` + undangan hidup (terbuka dan `expires_at > now`), dihitung dari baris sebenarnya lewat satu query, dan didaftarkan composition root sebagai counter `members`. `org` memanggil interface `LimitChecker` (dipenuhi `entitlement.Service`, dipasang terlambat di `app.go` karena entitlement menghitung kursi lewat `org`). Di `Invite`, dalam transaksi yang sama: baris organisasi dikunci (`FOR UPDATE`, undangan paralel berurutan), lalu `CheckLimit(members, 1)` **sebelum** upsert. Kursi baru hanya bila undangan itu benar-benar menambah kursi: undangan baru, atau menyegarkan undangan terbuka yang **sudah kedaluwarsa** (kursinya sudah lepas). Menyegarkan undangan yang masih hidup tidak minta kursi. `resend` mengikuti aturan yang sama: mengirim ulang undangan kedaluwarsa mengambil kursi lagi (sedikit lebih ketat dari yang tersirat §7.4, supaya "anggota + undangan aktif" tidak bisa dilampaui lewat resend). Menerima undangan tidak butuh kursi baru (kursi berubah dari undangan menjadi anggota); mencabut, menolak, dan kedaluwarsa melepas kursi.
- **`RequireFeature`.** `entitlement.Service.RequireFeature(feature)` adalah middleware gin untuk rute di bawah `org.TenantRoutes` (jalan setelah autentikasi dan tenant middleware, jadi bukan anggota tetap `404`). Ia membaca organisasi dari `reqctx.OrgID`, membuka transaksi baca sendiri yang terpaku ke tenant, dan menjawab `403 PLAN_FEATURE_UNAVAILABLE` (detail di atas) bila plan efektif tidak memuat fitur. Plan dibaca di tiap request (tanpa cache), jadi upgrade, kedaluwarsa, atau grace yang habis langsung berlaku. Rute tanpa tenant (kesalahan wiring) menjawab `500`, tidak pernah membuka fitur. Belum ada rute produksi yang digerbang fitur; contoh nyata (`/exports/members.csv` dengan `export_csv`, `/audit-log` dengan `audit_log`) hanya ada di test lewat `app.Options.TenantRoutes`.
- **`GET /plans`** (publik, tanpa login): `{"items": [...]}` berisi plan aktif berurutan `sort_order`, tiap item `code`, `name`, `price_amount` (satuan terkecil), `currency`, `interval`, `trial_days`, `features` (`[]` bila kosong), `limits` (`-1` = tak terbatas). Tanpa ID internal, tanpa paginasi (katalog kecil).
- **`GET /orgs/{orgId}/subscription`** (semua anggota; rute di bawah `org.TenantRoutes`, jadi bukan anggota `404`): `{"subscription": {...}}` dengan `plan` (kode plan subscription), `status`, `trial_ends_at`, `current_period_start`, `current_period_end` (`null` untuk Free), `cancel_at_period_end`, `pending_plan` (kode plan, downgrade terjadwal), `grace_until`, `effective_plan` (kode plan yang sedang boleh dipakai; `free` bila subscription tidak lagi memberi akses) dan `usage` (`{"members": {"used": 2, "max": 3}}`, `max` `-1` = tak terbatas, dihitung terhadap limit plan efektif). Waktu dalam UTC. Satu transaksi baca terpaku ke tenant. Organisasi tanpa baris subscription (tidak seharusnya ada) menjawab `404`.
- **Seed dev.** `make seed` (`cmd/seed`, flag `-config`; logika di `internal/seed` supaya teruji) menolak `app.env=production`, memeriksa bahwa plan `free`, `pro`, `business` ada (plan berasal dari migrasi `00008`, bukan dari seed; yang hilang berarti migrasi belum dijalankan), lalu membuat akun demo `demo@example.com` (email terverifikasi, password dev publik `demo-password-123` yang dicetak) beserta organisasi `demo` di plan Free, lewat service yang sama dengan API. Idempoten: bila akun demo sudah ada, tidak ada yang diubah (seed bukan reset).

