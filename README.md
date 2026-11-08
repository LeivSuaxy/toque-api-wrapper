# toque-api-provider

API en Go para recolectar, cachear y servir las tasas de ElToque. Incluye autenticación por api key + secret, rate limit por key, trazabilidad de consumo y un panel de administración renderizado con Go.

## Características

- Recolección automática de ElToque cada 5 minutos (configurable).
- Persistencia SQLite (WAL) con retención de tasas y logs.
- `GET /v1/rates` con `fixed=true` (mediana de los últimos 25) o `fixed=false` (último registro).
- Autenticación con `X-API-Key` + `X-API-Secret`.
- Rate limit por api key (RPM configurable por key).
- Registro de cada llamada autenticada: bytes de request/response, status, duración, IP y user agent.
- Registro separado de intentos de autenticación fallidos.
- Panel admin con login, creación/revocación/rotación de keys y consumo 24h/7d/30d.
- Vista de diagnóstico con tasas, errores de sincronización, peticiones API y fallos de autenticación; refresco automático cada 30s y recolección manual.
- HTML/CSS/JS embebidos con `//go:embed`; sin Node ni CDN.
- Sin framework web; solo `net/http` + `modernc.org/sqlite`.

## Requisitos

- Go 1.27+

## Instalación

```bash
go mod tidy
```

## Configuración

```bash
cp .env.example .env
```

Edita `.env`. Variables principales:

| Variable | Default | Descripción |
|---|---|---|
| `ELTOQUE_API_KEY` | — (req.) | Bearer token de ElToque |
| `ELTOQUE_BASE_URL` | `https://tasas.eltoque.com/v1/trmi` | Endpoint de ElToque |
| `PORT` | `8080` | Puerto de escucha HTTP |
| `HTTP_ADDR` | `:8080` | Dirección HTTP (tiene prioridad sobre `PORT`) |
| `DATABASE_PATH` | `./data/toque.db` | Ruta SQLite |
| `FETCH_INTERVAL` | `5m` | Intervalo del collector |
| `SAMPLE_SIZE` | `25` | Registros para la mediana |
| `RETENTION` | `720h` | Retención de tasas (30 días) |
| `REQUEST_LOG_RETENTION` | `2160h` | Retención de `api_requests` (90 días) |
| `AUTH_FAILURE_RETENTION` | `720h` | Retención de fallos de auth |
| `SUPERADMIN_USERNAME` | — (req.) | Usuario del panel |
| `SUPERADMIN_PASSWORD` | — | Contraseña en claro (opción A) |
| `SUPERADMIN_PASSWORD_HASH` | — | SHA-256 hex de la contraseña (opción B, recomendada) |
| `SESSION_SECRET` | random | Firma HMAC de la cookie de sesión |
| `API_SECRET_PEPPER` | — (req.) | Pepper para hashear los api secrets |
| `ADMIN_SESSION_TTL` | `12h` | Duración de la sesión admin |
| `COOKIE_SECURE` | `true` | Cookie solo HTTPS (pon `false` en local) |
| `RATE_LIMIT_PER_MINUTE` | `60` | RPM por defecto para nuevas keys |
| `RATE_LIMIT_BURST` | `60` | Burst global; se limita al RPM de cada key |
| `LOGIN_RATE_LIMIT_PER_MINUTE` | `5` | Intentos de login por IP |
| `TRUST_PROXY_HEADERS` | `false` | Usar `X-Forwarded-For` detrás de proxy |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

### Contraseña del superadmin con SHA-256

El servicio usa `SUPERADMIN_PASSWORD_HASH` si está definido; si no, `SUPERADMIN_PASSWORD`.

```bash
# Linux
printf '%s' 'TU_PASSWORD' | sha256sum | cut -d' ' -f1

# macOS
printf '%s' 'TU_PASSWORD' | shasum -a 256 | cut -d' ' -f1
```

```env
SUPERADMIN_USERNAME=admin
SUPERADMIN_PASSWORD_HASH=<hash_hex>
```

No uses `echo` (añade un salto de línea y cambia el hash). Usa `printf '%s'`.

## Ejecutar

```bash
go run ./cmd/api
```

- Panel: http://localhost:8080/admin
- Health: http://localhost:8080/healthz

## API

### Headers obligatorios

```http
X-API-Key: tk_live_...
X-API-Secret: ...
```

### `GET /v1/rates`

| Query | Default | Descripción |
|---|---|---|
| `fixed` | `true` | `true`: mediana de los últimos `SAMPLE_SIZE`; `false`: último registro |
| `currency` | todas | Filtro por código (`USD`, `ECU`, `MLC`, `TRX`, `USDT_TRC20`, `BTC`, ...) |

```bash
curl -H "X-API-Key: $KEY" -H "X-API-Secret: $SECRET" \
  'http://localhost:8080/v1/rates'

curl -H "X-API-Key: $KEY" -H "X-API-Secret: $SECRET" \
  'http://localhost:8080/v1/rates?fixed=false&currency=USD'
```

Respuesta:

```json
{
  "fixed": true,
  "sample_size": 25,
  "generated_at": "2026-11-08T15:30:00Z",
  "data": [
    {
      "currency_code": "USD",
      "rate": 920,
      "records_used": 5,
      "source_date": "2026-11-08 09:20:00",
      "min": 900,
      "max": 940,
      "latest_rate": 940
    }
  ]
}
```

Errores: `401` sin credenciales válidas, `429` rate limit, `400` `fixed` inválido, `404` moneda desconocida, `503` sin datos.

### Rate limit

Cada key tiene su RPM. La respuesta incluye:

```http
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 59
X-RateLimit-Reset: <unix>
Retry-After: 60   # solo en 429
```

### `GET /healthz`

Público. Devuelve estado de DB, cantidad de registros y última fecha recolectada.

## Panel de administración

1. Entra a `/admin` e inicia sesión con el superadmin de las variables de entorno.
2. Crea una key indicando nombre y RPM.
3. El **secret se muestra una sola vez**. Cópialo.
4. Desde el detalle puedes ajustar el RPM, revocar, reactivar o rotar el secret.
5. El dashboard muestra consumo agregado de las últimas 24 horas; el detalle muestra 24h/7d/30d.

### Diagnóstico

`/admin/diagnostics` muestra el estado de la base de datos para depurar la app y las consultas a ElToque:

- Tasas más recientes por moneda y últimas tasas recolectadas (filtrable por moneda).
- Errores de sincronización (`sync_errors`) con su detalle.
- Peticiones API (`api_requests`) y fallos de autenticación (`auth_failures`), filtrables por key.
- Botón **Recolectar ahora** para ejecutar el collector al instante.
- La página se refresca automáticamente cada 30 segundos; también hay botón **Actualizar**.

## Trazabilidad

Cada llamada autenticada a `/v1/*` se registra en `api_requests`:

- `api_key_id`, método, path, query, status.
- `request_bytes`, `response_bytes`, `duration_ms`.
- `remote_ip`, `user_agent`, `created_at`.

Los intentos con key/secret inválidos o keys revocadas se registran en `auth_failures` (sin guardar el secret).

## Docker

```bash
docker compose up --build
```

La base de datos se persiste en el volumen `toque-data` (`/data/toque.db`). El servicio escucha en el puerto definido por `PORT` (por defecto `8080`); el puerto publicado en el host se puede ajustar con `TOQUE_API_PORT`.

> En producción termina TLS en un reverse proxy y deja `COOKIE_SECURE=true`.

## CI/CD (GitHub Actions → VPS)

El workflow `.github/workflows/deploy.yml` se dispara con cada push a `main` (y manualmente):

1. Ejecuta `go vet` y `go test`.
2. Construye y publica la imagen en GHCR (`toque-api-latest` y `toque-api-<sha>`).
3. Copia `docker-compose.yml` a la VPS y despliega con `docker compose` en `/docker/toque-api`.
4. Espera a que el healthcheck del contenedor esté `healthy`.

### Secrets del repositorio

| Secret | Descripción |
|---|---|
| `VPS_HOST` | IP o host de la VPS |
| `VPS_USER` | Usuario SSH |
| `VPS_SSH_KEY` | Clave privada SSH |
| `VPS_SSH_KEY_PASSPHRASE` | Passphrase de la clave (si tiene) |
| `GHCR_PULL_TOKEN` | PAT con `read:packages` para que la VPS descargue la imagen |
| `ELTOQUE_API_KEY` | Bearer token de ElToque |
| `SUPERADMIN_USERNAME` | Usuario del panel |
| `SUPERADMIN_PASSWORD_HASH` | SHA-256 hex de la contraseña del panel |
| `SESSION_SECRET` | Secreto para firmar la cookie de sesión |
| `API_SECRET_PEPPER` | Pepper para hashear los api secrets |

### Variables del repositorio (opcionales)

| Variable | Default |
|---|---|
| `PORT` | `8080` |
| `TOQUE_API_PORT` | igual a `PORT` |
| `ELTOQUE_BASE_URL` | `https://tasas.eltoque.com/v1/trmi` |
| `COOKIE_SECURE` | `false` (usa `true` detrás de HTTPS) |
| `TRUST_PROXY_HEADERS` | `false` |
| `LOG_LEVEL` | `info` |
| `FETCH_INTERVAL` | `5m` |
| `SAMPLE_SIZE` | `25` |
| `RETENTION` | `720h` |
| `REQUEST_LOG_RETENTION` | `2160h` |
| `AUTH_FAILURE_RETENTION` | `720h` |
| `RATE_LIMIT_PER_MINUTE` | `60` |
| `RATE_LIMIT_BURST` | `60` |
| `LOGIN_RATE_LIMIT_PER_MINUTE` | `5` |
| `ADMIN_SESSION_TTL` | `12h` |

Notas:

- Si el paquete de GHCR es público, puedes dejar `GHCR_PULL_TOKEN` vacío; el workflow omite el login.
- El workflow usa `COOKIE_SECURE=false` por defecto para que el login funcione por HTTP directo. Si publicas el panel detrás de HTTPS, define la variable `COOKIE_SECURE=true`.
- El volumen de SQLite se deriva del nombre del directorio de despliegue. Mantén `DEPLOY_DIR` estable (`/docker/toque-api`) para conservar la base de datos.
- El `docker-compose.yml` sirve para local (`docker compose up --build`) y para producción (imagen de GHCR + `--no-build`).

## Pruebas

```bash
go test ./...
go vet ./...
gofmt -l .
```

## Notas

- No cambies `API_SECRET_PEPPER` después de crear keys: invalida todos los secrets existentes.
- Si `SESSION_SECRET` se deja vacío, el proceso genera uno efímero y las sesiones admin se pierden al reiniciar.
- ElToque devuelve la hora local de Cuba (`America/Havana`); el collector usa esa zona con `time/tzdata` embebido.
- Se almacenan todas las claves de `tasas`, incluyendo `BTC` si viene en la respuesta.
- El rate limit y las sesiones viven en memoria: se reinician al reiniciar el proceso. Para múltiples réplicas haría falta un almacén compartido.
- La API no debe exponerse sin TLS si se usa `COOKIE_SECURE=true`.
