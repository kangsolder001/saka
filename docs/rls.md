# Row Level Security — temuan spike dan desain

> Sumber temuan: `internal/platform/db/rls_spike_integration_test.go` (tabel dummy, 1 juta baris, 100 tenant, PostgreSQL 18). Test itu menjaga agar catatan di sini tetap benar.

## Temuan

1. **Pemilik tabel dan superuser mengabaikan policy.** Container dev dan test memakai user superuser, jadi RLS tidak berlaku bila aplikasi konek langsung dengan user itu. Solusinya: setiap koneksi aplikasi menjalankan `SET ROLE <app_role>` (hook `AfterConnect` pada pool). `current_user` berubah menjadi role non-owner, non-superuser, dan policy berlaku. Tidak perlu user login terpisah di dev.
2. **Tanpa `SET LOCAL`, role aplikasi melihat nol baris, bukan error.** Lupa menyetel tenant gagal secara aman.
3. **Query tanpa filter `organization_id` hanya mengembalikan baris tenant aktif.** Inilah bug yang dijaga RLS (`SELECT count(*) FROM ...` tanpa `WHERE`).
4. **Setting tidak bocor antar request di koneksi pool**, asalkan dipasang dengan `set_config(name, value, true)` (setara `SET LOCAL`) di dalam transaksi. Jebakan: setelah transaksi selesai, `current_setting('app.current_org', true)` mengembalikan string kosong `''`, **bukan NULL**. Policy yang langsung mem-cast ke `uuid` meledak dengan `invalid input syntax for type uuid` pada request berikutnya. Policy wajib memakai `NULLIF(current_setting(...), '')::uuid`.
5. **INSERT/UPDATE ke tenant lain ditolak** (`new row violates row-level security policy`) karena `USING` berfungsi juga sebagai `WITH CHECK`. `UPDATE`/`DELETE` terhadap baris tenant lain tidak error: 0 baris terpengaruh. Pernyataan yang ditolak menggugurkan transaksinya.
6. **Overhead tidak terukur pada 1 juta baris** (index `(organization_id, id)`; hasil setelah warm-up):

   | Query | Tanpa RLS | RLS naif | RLS dengan `(SELECT ...)` |
   |---|---|---|---|
   | Satu halaman (50 baris) milik satu tenant | 0,07 ms | 0,07 ms | 0,08 ms |
   | `count(*)` tanpa filter org (10.000 baris tenant) | 15,5 ms (dengan filter) | 15,1 ms | 16,0 ms |

   Predikat policy tetap dipakai sebagai `Index Cond` pada index berawalan `organization_id`, tidak ada `Seq Scan`. Ini alasan aturan "index tabel org-scoped diawali `organization_id`".
7. **Bentuk `(SELECT ...)` tetap dipakai.** PostgreSQL 18 sudah cukup pintar untuk bentuk naif pada query sederhana, tetapi bentuk `(SELECT NULLIF(...))` menjadi InitPlan yang dievaluasi sekali per query secara eksplisit, tidak bergantung pada versi atau bentuk query.

## Desain yang dipakai

- **Role.** Migrasi membuat role `saka_app` (`NOLOGIN`, tanpa `BYPASSRLS`) dan memberinya hak DML. Konfigurasi `database.app_role` (default `saka_app`) membuat pool menjalankan `SET ROLE saka_app` pada tiap koneksi. Migrasi berjalan sebagai pemilik tabel.
- **Tenant.** `db.WithTx` menyetel `app.current_org` dari `reqctx.OrgID(ctx)` (hanya diisi tenant middleware). Policy: `organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)`.
- **Jalur bypass eksplisit.** Alur yang memang lintas tenant (daftar organisasi milik user, undangan lewat token, registrasi, cron billing, worker) memakai `db.WithSystemTx`, yang menyetel `app.bypass_rls = 'on'`. Policy menerima kondisi ini. Jalur ini sengaja sempit dan mudah dicari (`grep WithSystemTx`); RLS menjaga dari query yang lupa filter, bukan dari kode yang sengaja meminta bypass.
- **Query di luar transaksi** (lewat pool) pada tabel ber-RLS mengembalikan nol baris. Kode org-scoped selalu berjalan di dalam `WithTx`.

## Aturan pakai (ringkas)

- Kode per organisasi: `s.tx.WithTx(ctx, ...)` dengan `ctx` dari tenant middleware. Jangan memakai pool langsung untuk tabel ber-RLS.
- Lintas tenant: `WithSystemTx` (transaksi baru) atau `db.WithBypass(ctx, tx, fn)` (satu langkah di transaksi yang sudah ada, mis. registrasi). Query di dalamnya wajib punya filter eksplisit.
- Organisasi baru: `db.PinTenant(ctx, tx, newID)` sebelum memasukkan barisnya.
- `FOR UPDATE`/`FOR SHARE` pada baris tenant tetap tunduk pada RLS: tanpa tenant atau bypass, baris tidak terlihat sehingga tidak ada yang terkunci (ditemukan lewat `TestGuard_JoiningWaitsForTheOrganizationLock`).
- Test: pool aplikasi (`db.Connect`) berjalan sebagai `saka_app`; susun dan periksa data lewat `dbtest.AdminPool`.
