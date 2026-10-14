# Plan Implementasi — SaaS Boilerplate Tier 1

> Sumber: [spec.md](spec.md) (Draft v0.1)
> Asumsi: 1 backend developer dibantu AI agent, dikerjakan **di sela waktu luang**, tanpa deadline tanggal. Timeline berupa durasi kerja efektif (1 hari = 1 hari penuh fokus).
> Detail tugas dan timeline per item: [todo.md](todo.md)

---

## 1. Strategi

Bangun dari bawah ke atas mengikuti ketergantungan modul, bukan mengikuti urutan bab spec:

```
platform ─▶ mail/outbox ─▶ auth ─▶ user ─▶ org ─▶ entitlement ─▶ billing ─▶ hardening
```

Alasan urutan:
1. **Outbox + mail sebelum auth**, karena registrasi, verifikasi, dan reset password langsung butuh email lewat outbox.
2. **Org sebelum billing**, karena subscription menempel ke organisasi dan registrasi membuat org pertama. Bagian registrasi yang membuat org/subscription di-stub dulu di Fase 2, lalu dilengkapi di Fase 4 dan 6.
3. **Entitlement sebelum billing**, supaya limit anggota (undangan) bisa dipakai plan `free` yang di-seed, dan billing tinggal mengubah plan efektif.
4. **Billing paling akhir** karena paling berisiko (gateway eksternal, webhook, cron, state machine). Diberi durasi terpanjang (10 hari).

## 2. Fase & Milestone

| Fase | Nama | Durasi | Milestone (definisi selesai) |
|---|---|---|---|
| 0 | Setup & Platform | 3 hari | API kosong jalan, `/healthz` OK, migrasi & CI berjalan, error format & middleware dasar siap |
| 1 | Outbox & Mail | 3 hari | Job email masuk outbox, worker mengirim ke Mailpit, retry backoff teruji |
| 2 | Auth | 6 hari | Register → verifikasi → login → refresh → reset password end-to-end |
| 3 | User | 3 hari | Profil, ganti password/email, sesi, hapus akun |
| 4 | Org, Tim & Role | 8 hari | Tenant middleware, RLS, undangan, role, transfer owner, tes isolasi tenant |
| 5 | Entitlement | 3 hari | Plan ter-seed, `CheckLimit` memblokir anggota melebihi limit |
| 6 | Billing | 10 hari | Checkout sandbox Midtrans, webhook idempoten, state machine, cron, reconcile |
| 7 | Hardening & Acceptance | 4 hari | Seluruh checklist §12 spec tercentang, observability, dokumentasi |
| – | Buffer | 5 hari | Cadangan bug / review |
| | **Total** | **40 hari efektif + 5 buffer** | |

**Konversi ke kalender:** dengan ±2 jam/hari, 1 hari efektif ≈ 3–4 hari kalender, jadi total sekitar 4–6 bulan. Dengan 2 hari penuh tiap akhir pekan, sekitar 20 minggu (±5 bulan).

Karena dikerjakan di sela waktu luang, tiap fase sengaja berakhir pada milestone yang bisa dijalankan dan diuji, sehingga aman berhenti di antara fase dan lanjut kapan saja.

## 3. Keputusan terbuka (§14 spec)

| # | Keputusan | Status |
|---|---|---|
| 1 | Gateway pembayaran | **Midtrans** (diputuskan) |
| 2 | Registrasi Free atau trial Pro | **Free**, trial dipicu saat pilih plan (diputuskan) |
| 3 | RLS PostgreSQL sejak awal | **Ya**, sekalian belajar (diputuskan) |
| 4 | Frontend | **Belum ditentukan, API agnostik**; tidak memblokir Tier 1 |
| 5 | Penomoran invoice | **Global** (diputuskan) |

**Database (diputuskan):** PostgreSQL 18 (UUID v7 lewat `uuidv7()` bawaan).

**Library (diputuskan):** `gin` (HTTP), `pgx` (driver PostgreSQL), `sqlc` (query SQL type-safe, tanpa ORM), `goose` (migrasi), `zerolog` (logger), `testcontainers-go` (integration test).

**Provider email produksi:** ditunda, diputuskan di Fase 7. Dev memakai Mailpit lewat interface `Mailer`.

## 4. Risiko

| Risiko | Dampak | Mitigasi |
|---|---|---|
| Akun sandbox gateway lambat disetujui | Fase 6 tertahan | Daftar akun sandbox di Fase 4, jauh sebelum Fase 6 |
| Perilaku webhook gateway berbeda dari dokumentasi | Rework billing | Spike kecil 0,5 hari di awal Fase 6 sebelum menulis modul |
| Isolasi tenant bocor | Insiden keamanan | Test isolasi dari awal Fase 4, RLS sebagai lapisan kedua, tiap repo wajib `orgID` dari context |
| Cron tidak idempoten / race | Invoice ganda | `FOR UPDATE SKIP LOCKED`, test jalan dua kali, clock yang bisa dimanipulasi sejak Fase 0 |
| Pengerjaan terputus-putus (waktu luang) | Konteks hilang, lupa progres | Berhenti hanya di batas fase atau tugas; centang todo.md; tiap fase punya milestone yang bisa diuji |
| Scope creep ke item §13 | Proyek tak kunjung selesai | Tolak; catat ke backlog Tier 2 |

## 5. Strategi Testing

- **Unit test** service layer dengan fake repo & fake clock (ditulis bersamaan dengan tiap modul).
- **Integration test** dengan PostgreSQL asli (testcontainers): repository, webhook, isolasi tenant, cron.
- **Definition of Done per tugas:** kode + test lulus + migrasi terversi + endpoint terdokumentasi di spec/README.
- CI menjalankan lint, unit test, dan integration test di setiap PR sejak Fase 0.

## 6. Urutan Migrasi

1. `users`, `user_tokens`, `refresh_tokens`, `outbox_jobs` (Fase 0–2)
2. `organizations`, `memberships`, `invitations` (Fase 4)
3. `plans` (+ seed), `subscriptions` (Fase 5)
4. `invoices`, `payment_events` (Fase 6)
5. Policy RLS (Fase 4, setelah tabel org-scoped ada)

> Catatan: `subscriptions` baru dibuat di Fase 5, jadi registrasi (Fase 2) hanya membuat user; pembuatan org + membership ditambahkan di Fase 4 dan subscription `free` di Fase 5. Acceptance criteria "daftar → otomatis punya org dan plan Free" baru lengkap di akhir Fase 5.

## 7. Definisi Selesai Tier 1

Semua checkbox di §12 spec tercentang dan terhubung ke test otomatis, `seed` dev berjalan, dan README menjelaskan cara menjalankan API + worker.
