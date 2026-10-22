# Panduan: menambah modul domain baru

Boilerplate ini dibuat supaya produk Anda tinggal menambahkan modul domain di atas fondasi (auth, organisasi, plan). Panduan ini adalah resep langkah demi langkah dengan satu contoh konkret: resource **`projects`** milik sebuah organisasi, dibatasi plan (`projects` maksimum 2 di Free, tak terbatas di Pro) dan satu endpoint ekspor yang hanya untuk plan dengan fitur `export_csv`.

**Resepnya sudah dibuktikan, bukan hanya ditulis**: `internal/app/example_module_integration_test.go` membangun modul `projects` ini persis mengikuti langkah di bawah di atas router asli (migrasi, counter, route, limit, role, fitur) dan menguji isolasi tenant, limit di bawah balapan, dan gating fitur. Kode di sana sengaja berada di berkas test (tidak ikut dikirim); modul sungguhan memakai `sqlc` dan paket sendiri seperti di bawah. Nama fungsi dan tipe yang dikutip di sini ada di kode (`grep` saja).

## Aturan yang tidak boleh dilanggar

1. **Organisasi adalah pemilik data.** Setiap tabel domain punya `organization_id`; query tanpa `organization_id` adalah bug.
2. **Tenant hanya dari `context.Context`** (`reqctx.OrgID`, diisi tenant middleware), tidak pernah dari body, query atau path yang bisa dipalsukan. Role juga hanya dari context (`org.RoleFrom` / `org.Authorize`).
3. **RLS PostgreSQL wajib** sebagai lapis kedua, dan index diawali `organization_id`. Penjaganya adalah test, bukan ingatan.
4. **Limit dicek di service, dalam transaksi yang sama dengan pembuatan resource**, dihitung dari data nyata (`COUNT`), tidak dari counter tersimpan.
5. **Semua operasi multi-tabel dalam satu transaksi**; email lewat outbox dalam transaksi yang sama; goroutine hanya lewat `platform/safego`; waktu lewat `platform/clock`.
6. **Modul hanya memanggil modul lain lewat interface** yang dideklarasikan pemakainya; jangan mengimpor paket `...db` modul lain dan jangan membaca tabel modul lain.

## Langkah 1: migrasi (tabel, RLS, policy, index)

```bash
make migrate-new name=create_projects     # membuat migrations/000NN_create_projects.sql
```

```sql
-- +goose Up
-- +goose StatementBegin
CREATE TABLE projects (
    id              uuid        PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid        NOT NULL REFERENCES organizations (id) ON DELETE CASCADE,
    name            text        NOT NULL CHECK (char_length(name) BETWEEN 1 AND 100),
    created_by      uuid        NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Diawali organization_id: predikat policy menjadi Index Cond, bukan seq scan (docs/rls.md).
CREATE INDEX projects_organization_created_idx ON projects (organization_id, created_at DESC, id);

-- Policy yang sama dengan semua tabel tenant (migrasi 00007). NULLIF + sub-select itu wajib:
-- setting yang sudah berakhir terbaca '' (bukan NULL) dan harus dievaluasi sekali per query.
ALTER TABLE projects ENABLE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON projects
    USING (
        organization_id = (SELECT NULLIF(current_setting('app.current_org', true), '')::uuid)
        OR (SELECT COALESCE(current_setting('app.bypass_rls', true), '') = 'on')
    );
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP POLICY tenant_isolation ON projects;
DROP TABLE projects;
-- +goose StatementEnd
```

Catatan:

- Hak akses ke role aplikasi `saka_app` tidak perlu ditulis: migrasi `00007` memasang *default privileges*, jadi tabel baru otomatis bisa dipakai role itu. Tabel baru **tanpa policy** akan dibaca penuh oleh siapa pun yang punya hak, karena itu langkah berikut penting.
- Waktu `timestamptz` (UTC), uang integer satuan terkecil, ID `uuidv7()`.
- **Penjaga otomatis** (`migrations/migrations_integration_test.go`): `TestTenantTables_AllHaveRowLevelSecurityAndAPolicy` otomatis memeriksa **setiap** tabel yang punya kolom `organization_id`: RLS menyala dan ada policy; tabel baru Anda gagal di sana bila lupa. Tambahkan nama tabel ke daftar `assert.Subset` di tes itu dan ke peta di `TestTenantTables_HaveAnIndexLedByOrganizationID` supaya index-nya ikut dijaga.
- Menghapus resource saat langganan berakhir **tidak boleh**: data tidak dihapus, hanya aksesnya yang dibatasi (spec §8.2). Penghapusan organisasi adalah soft delete.

## Langkah 2: query `sqlc`

Daftarkan modul di `sqlc.yaml` (salin salah satu entri, mis. `internal/org`): `queries: internal/projects/queries`, `package: projectsdb`, `out: internal/projects/projectsdb`. Tulis `internal/projects/queries/projects.sql`:

```sql
-- name: CountProjects :one
SELECT count(*) FROM projects WHERE organization_id = @organization_id;

-- name: InsertProject :one
INSERT INTO projects (organization_id, name, created_by)
VALUES (@organization_id, @name, @created_by)
RETURNING *;

-- name: ListProjects :many
SELECT * FROM projects
WHERE organization_id = @organization_id
  AND (created_at, id) < (@after_created_at, @after_id)   -- keyset; lihat internal/org/queries untuk bentuk lengkap
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: GetProject :one
SELECT * FROM projects WHERE organization_id = @organization_id AND id = @id;
```

Aturannya: **setiap query menyebut `organization_id`** (juga `GetProject`: ID saja tidak cukup, lihat "rute" di bawah), tanpa ORM, tanpa SQL yang dirakit dari string (`TestSQL_NoStatementIsBuiltFromStrings` menolaknya). Jalankan `make sqlc`; hasil `projectsdb` di-commit (CI menolak selisih).

## Langkah 3: service yang mengambil tenant dari context dan memakai `WithTx`

```go
// Dideklarasikan di modul projects, dipenuhi *db.DB (konvensi repo: interface milik pemakainya).
type Transactor interface {
    WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

func (s *Service) Create(ctx context.Context, name string) (Project, error) {
    if err := org.Authorize(ctx, org.ActionUpdateSettings); err != nil { // Langkah 5
        return Project{}, err
    }
    orgID, ok := reqctx.OrgID(ctx) // diisi tenant middleware; BUKAN dari input
    if !ok {
        return Project{}, apperr.NotFound()
    }
    userID, ok := reqctx.UserID(ctx)
    if !ok {
        return Project{}, apperr.Unauthenticated()
    }
    var created Project
    err := s.tx.WithTx(ctx, func(tx pgx.Tx) error { // menyetel SET LOCAL app.current_org => RLS berlaku
        if err := org.LockOrganization(ctx, tx, orgID); err != nil { // Langkah 4: serialisasi
            return err
        }
        if err := s.limits.CheckLimit(ctx, tx, orgID, LimitProjects, 1); err != nil {
            return err // PLAN_LIMIT_REACHED (402) lengkap dengan detail upgrade
        }
        row, err := projectsdb.New(tx).InsertProject(ctx, projectsdb.InsertProjectParams{
            OrganizationID: orgID, Name: name, CreatedBy: userID,
        })
        if err != nil {
            return fmt.Errorf("projects: insert: %w", err)
        }
        created = projectFromRow(row)
        return nil
    })
    return created, err
}
```

- `db.DB.WithTx` memasang tenant dari `ctx` (hanya bila tenant middleware yang mengisinya). Jangan memakai `pool` langsung untuk tabel ber-RLS: hasilnya nol baris (aman, tetapi bukan yang Anda maksud). Alur lintas tenant yang memang disengaja memakai `WithSystemTx` dan wajib punya filter eksplisit (jarang; mudah dicari dengan `grep WithSystemTx`).
- Fungsi repository menerima `db.DBTX` (pool atau transaksi) supaya bisa dipakai dalam transaksi pemanggil.
- Jangan paralelkan operasi dalam satu transaksi (`pgx.Tx` tidak aman untuk akses bersamaan).
- Method **baca** (`List`, `Get`) memakai `org.ActionView`, `WithTx` dan filter `organization_id` yang sama.

## Langkah 4: `entitlement.CheckLimit` di transaksi pembuatan

Tiga hal harus ada:

1. **Limit ada di data plan, bukan di kode.** Tambahkan ke `plans.limits` lewat migrasi (mis. `UPDATE plans SET limits = limits || '{"projects": 2}' WHERE code = 'free'`, dan `-1` = tak terbatas untuk plan lain). Limit yang tidak tercantum di plan efektif dianggap `0` (tertutup).
2. **Daftarkan counter** di composition root (`internal/app/app.go`), di sebelah `entitlements.RegisterCounter(org.MembersLimit, organizations.CountSeats)`:

   ```go
   entitlements.RegisterCounter(projects.LimitProjects, projectsService.Count) // func(ctx, q db.DBTX, orgID uuid.UUID) (int, error)
   ```

   `Count` menjalankan `CountProjects` (data nyata, bukan counter tersimpan). Limit tanpa counter adalah error internal; nama ganda adalah panic saat start (bug wiring).
3. **Kunci baris organisasi dulu, lalu cek, lalu insert**, semuanya dalam satu transaksi: `org.LockOrganization(ctx, tx, orgID)` (sama seperti `org.Invite` untuk limit `members`). `CheckLimit` sendiri tidak mengunci apa pun, jadi tanpa kunci dua request yang berebut slot terakhir sama-sama lolos. Test contoh membuktikannya: tanpa kunci, 6 dari 6 pembuat lolos melewati limit 2 (menghasilkan 7 proyek); dengan kunci tepat satu.

Interface untuk modul Anda sendiri, jangan mengimpor `entitlement` ke paket lain bila bisa dihindari:

```go
type LimitChecker interface {
    CheckLimit(ctx context.Context, q db.DBTX, orgID uuid.UUID, limit string, delta int) error
}
```

`delta <= 0` tidak pernah ditolak (organisasi yang kelebihan setelah downgrade tetap bisa menghapus). Detail error `PLAN_LIMIT_REACHED` (`limit`, `max`, `used`, `plan`, `upgrade_to`) sudah diisi `entitlement`.

## Langkah 5: otorisasi dengan `org.Authorize`

Matriks izin ada di `internal/org/authz.go` (`permissions`, sumbernya spec §7.2). Pakai aksi yang paling sesuai (`org.ActionView` untuk membaca, `org.ActionUpdateSettings` untuk menulis oleh owner dan admin, `org.ActionManageBilling` hanya owner). Panggil `org.Authorize(ctx, action)` **di service** (supaya pemanggil non-HTTP pun terjaga) dan/atau pasang `org.Require(action)` pada route.

Butuh aksi baru (mis. `ActionManageProjects`)? Tambahkan konstanta dan barisnya di `permissions`, perbarui matriks di spec §7.2, dan sel-sel di `TestCan_MatchesThePermissionMatrixCellByCell` (`internal/org/authz_test.go`), yang menggagalkan build bila matriks dan tes tidak sama.

## Langkah 6: rute lewat `org.TenantRoutes`

Handler modul menerima grup yang sudah melewati autentikasi dan tenant middleware (bukan anggota = `404`, `orgId` bukan UUID = `404`):

```go
func (h *Handler) Routes(tenant *gin.RouterGroup, requireFeature func(string) gin.HandlerFunc) {
    tenant.POST("/projects", h.create)
    tenant.GET("/projects", h.list)
    tenant.GET("/projects/:id", h.get)
    tenant.GET("/projects/export", requireFeature("export_csv"), h.export) // Langkah 7
}
```

Pasang di `internal/app/app.go`, setelah `tenant := org.TenantRoutes(...)` (baris yang juga memasang billing):

```go
projects.NewHandler(projectsService).Routes(tenant, entitlements.RequireFeature)
```

Aturan handler: `httpx.BindJSON` untuk body (batas 1 MiB, field tak dikenal ditolak; tiap string diberi `validate:"...,max=N"`), `uuid.Parse(c.Param("id"))` dan `404` bila gagal, `httpx.Error(c, err)` untuk error, paginasi dengan `pagination.ParseQuery` + `pagination.Paginate` (cursor, bukan offset). **Resource dicari dengan `organization_id` dan `id` sekaligus**: ID resource organisasi lain harus `404`, bukan `403`.

## Langkah 7: gating fitur dengan `RequireFeature`

`entitlements.RequireFeature("export_csv")` (middleware, jalan setelah tenant middleware) menjawab `403 PLAN_FEATURE_UNAVAILABLE` berisi `feature`, `plan` dan `upgrade_to`. Fitur ada di `plans.features` (data, bukan kode); plan dibaca tiap request sehingga upgrade, kedaluwarsa dan akhir masa tenggang langsung berlaku. Untuk pengecekan di dalam service gunakan `entitlements.CheckFeature(ctx, q, orgID, feature)`.

## Langkah 8: daftarkan rute di inventaris dan dokumentasi

Rute baru di bawah `/orgs/{orgId}` **harus** didaftarkan secara sadar:

- `TestTenantIsolation_TheOrganizationRouteInventory` (`internal/app/tenant_isolation_integration_test.go`) memuat daftar persis rute tenant; tambahkan rute baru ke sana. `TestTenantIsolation_EveryOrganizationRouteHidesItFromNonMembers` mengambil rute langsung dari router dan otomatis memastikan non-anggota mendapat `404` dan tanpa login `401`.
- Tambahkan operasi ke `docs/openapi.yaml`. `TestOpenAPI_DescribesExactlyTheRegisteredRoutes` gagal bila router dan dokumen berbeda (dua arah), dan `TestOpenAPI_SecuredOperationsRejectARequestWithoutAToken` memeriksa tanda keamanannya.

## Aturan cache, outbox, waktu, goroutine

- **Cache** (Redis) hanya untuk data turunan yang boleh hilang, lewat `redisx.Namespace("projects")` (key otomatis `<key_prefix>:projects:...`). Kunci cache data organisasi **wajib memuat `org_id`**. Pola cache-aside; **semua method tulis mengakhiri diri dengan penghapusan cache lewat `db.AfterCommit(tx, fn)`** (setelah commit, bukan sebelum), seperti `user.Service.invalidateAfterCommit`, dan wajib punya test yang membuktikan drop-after-commit, rollback dan tanpa transaksi. Data uang atau data yang harus tahan lama tidak boleh hanya di Redis.
- **Email** hanya lewat outbox: `mail.Sender.Send(ctx, tx, mail.Email{Template, To, Data})` di dalam transaksi yang sama dengan perubahan data (rollback membatalkan emailnya). Template baru: `templates/email/<kode>.html.tmpl` dan `.txt.tmpl`, ditambah baris data di `TestRealTemplates_AllRenderWithTheirData` (`internal/mail/renderer_test.go`). Jangan memegang atau memakai `mail.Mailer`: `TestMailer_*` menggagalkan build.
- **Waktu**: terima `clock.Clock` lewat constructor, jangan `time.Now()`. **Goroutine**: hanya `safego`, wajib berhenti saat `ctx` batal. **Logger**: inject lewat constructor, ambil dengan `log.FromContext(ctx)` supaya `trace_id`/`user_id`/`org_id` ikut; jangan mencatat token, password atau header `Authorization`.
- **Job periodik** (mis. pembersihan): daftarkan di `scheduler` worker, harus idempoten dan aman jalan bersamaan (`FOR UPDATE SKIP LOCKED`), seperti job di `internal/billing/jobs*.go`.

## Test yang wajib ada

| Apa | Contoh di contoh modul |
|---|---|
| Migrasi: RLS menyala, ada policy, index diawali `organization_id` | `TestExampleModule_TheMigrationFollowsTheTenantRules` + penjaga di `migrations/` |
| Isolasi tenant: org A tidak membaca/menulis data org B (via URL dan via ID yang diselundupkan), serta RLS menyembunyikan baris walau query lupa filter | `TestExampleModule_OrganizationsNeverSeeEachOthersProjects` |
| Limit: batas tepat, error berisi detail upgrade, kembali ke limit Free saat `expired`, **balapan** pada slot terakhir | `TestExampleModule_TheFreePlanLimitIsEnforcedInTheCreatingTransaction`, `TestExampleModule_RacingCreatorsOfTheLastSlotLetExactlyOneThrough` |
| Role: tiap aksi per role (owner/admin/member) dan tanpa login | `TestExampleModule_RolesAreCheckedInTheServiceAndTheRoute` |
| Fitur: `403 PLAN_FEATURE_UNAVAILABLE` di Free, lolos setelah upgrade | `TestExampleModule_FeatureGatedRoutesFollowThePlan` |
| Jalur gagal: rollback transaksi (mis. email gagal diantre membatalkan insert), validasi body, ID tak valid | ikuti pola `internal/org/*_integration_test.go` |
| Inventaris rute tenant dan OpenAPI | Langkah 8 |
| Unit test service dengan mock `mockery` (tambahkan interface ke `.mockery.yml`, jalankan `make mocks`) | `internal/auth/fixture_test.go` |

Jalankan `make test`, `make test-integration` dan `make lint`; `make tidy mocks sqlc` tidak boleh menghasilkan selisih.

## Daftar periksa

- [ ] Migrasi: `organization_id`, RLS + policy `tenant_isolation`, index diawali `organization_id`, `Down` yang benar
- [ ] `sqlc.yaml` + `queries/*.sql` (setiap query berfilter `organization_id`), `make sqlc`
- [ ] Service: `reqctx.OrgID`/`UserID` dari context, `org.Authorize`, semua tulis dalam `WithTx`
- [ ] Limit: di `plans.limits` (migrasi), counter terdaftar di `app.go`, `org.LockOrganization` + `CheckLimit` di transaksi pembuatan
- [ ] Rute lewat `tenant` (`org.TenantRoutes`), `BindJSON`, UUID diparse, `RequireFeature` bila digerbang
- [ ] Inventaris `TestTenantIsolation_TheOrganizationRouteInventory` dan `docs/openapi.yaml` diperbarui
- [ ] Tes: isolasi, role, limit (termasuk balapan), fitur, jalur gagal
- [ ] Cache (bila ada) dihapus setelah commit; email lewat outbox dalam transaksi yang sama
- [ ] Dokumentasi: spec (bagian modul baru) dan `docs/todo.md`
