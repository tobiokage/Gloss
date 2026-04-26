# Gloss

## Required Environment Variables

### API (`cmd/api`)
- `APP_ENV`
- `HTTP_PORT`
- `SHUTDOWN_TIMEOUT_SECONDS`
- `JWT_SECRET`
- `JWT_TTL_MINUTES`
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `DB_SSLMODE`
- `HDFC_BASE_URL`
- `HDFC_CLIENT_API_KEY`
- `HDFC_CLIENT_SECRET_KEY`
- `HDFC_AUTHORIZATION_TOKEN`
- `HDFC_IV`

### Migrate (`cmd/migrate`)
- `DB_HOST`
- `DB_PORT`
- `DB_USER`
- `DB_PASSWORD`
- `DB_NAME`
- `DB_SSLMODE`

## Local Commands

```powershell
go run ./cmd/migrate -command up
go run ./cmd/api
```

Dev seed data is not part of the production migration path. For local demo data, apply `schema/postgres/seeds/dev_seed.sql` explicitly after migrations.
