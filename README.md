# pulso-api

[![ci](https://github.com/MatiasHernanSerrato/pulso-api/actions/workflows/ci.yml/badge.svg)](https://github.com/MatiasHernanSerrato/pulso-api/actions/workflows/ci.yml)
[![docker](https://github.com/MatiasHernanSerrato/pulso-api/actions/workflows/docker.yml/badge.svg)](https://github.com/MatiasHernanSerrato/pulso-api/actions/workflows/docker.yml)
[![codecov](https://codecov.io/gh/MatiasHernanSerrato/pulso-api/branch/main/graph/badge.svg)](https://codecov.io/gh/MatiasHernanSerrato/pulso-api)
[![go report](https://goreportcard.com/badge/github.com/MatiasHernanSerrato/pulso-api)](https://goreportcard.com/report/github.com/MatiasHernanSerrato/pulso-api)

Backend de Pulso, un tracker personal que mezcla salud y plata. La idea
es tener una sola API que me deje registrar peso, actividad, gastos e
inversiones, y sacar graficos decentes sin tener que usar 4 apps
distintas.

Esto es un side project. Lo uso para probar cosas que en el laburo no
me dejan (TimescaleDB, GraphQL con directives de auth, Go con generics
bien usados).

## Indice

- [Arquitectura](#arquitectura)
- [Stack](#stack)
- [Correr local](#correr-local)
- [Variables de entorno](#variables-de-entorno)
- [Comandos de Make](#comandos-de-make)
- [Estructura](#estructura)
- [Auth](#autenticacion)
- [Modelo financiero](#modelo-financiero)
- [Deteccion de anomalias](#deteccion-de-anomalias)
- [Observabilidad](#observabilidad)
- [Docker y despliegue](#docker-y-despliegue)
- [Fly.io](#flyio)

## Arquitectura

<p align="center">
  <img src="docs/architecture.png" alt="diagrama de la arquitectura de pulso-api" width="900" />
</p>

El mismo diagrama en Mermaid por si Github no renderiza el PNG.

```mermaid
flowchart LR
  subgraph clients[Clientes]
    MOB[pulso-mobile<br/>Flutter]
    WEB[pulso-web<br/>Next.js RSC]
    CURL[curl / tests]
  end

  subgraph api[pulso-api]
    direction TB
    R[chi router]
    GQL[graphql<br/>gqlgen]
    MW[auth JWT<br/>middleware]
    WH[webhooks<br/>mercadopago]
    SVC[service layer<br/>weights / txns / alerts]
    MET[/metrics prometheus/]
    HLZ[/healthz/]
  end

  subgraph data[Datos]
    PG[(postgres 16<br/>+ timescaledb)]
    SQLC[sqlc<br/>queries tipadas]
    ANOM[anomaly<br/>z-score + alerts]
  end

  subgraph obs[Observabilidad]
    PROM[(prometheus)]
    GRAF[grafana]
  end

  MOB -->|HTTPS + JWT| R
  WEB -->|HTTPS + JWT| R
  CURL --> R

  R --> MW
  R --> HLZ
  R --> MET
  R --> WH

  MW --> GQL
  GQL --> SVC
  WH --> SVC
  SVC --> SQLC
  SQLC --> PG
  SVC --> ANOM
  ANOM --> PG

  MET -.scrape.-> PROM
  PROM --> GRAF
```

Si el diagrama de arriba no te renderea (algunos readers de markdown no
lo hacen), el flujo en una linea es: cliente -> router chi -> middleware
de auth JWT -> resolvers graphql -> service layer -> sqlc/pgx ->
postgres+timescaledb. Al costado quedan /healthz y /metrics sin auth, y
los webhooks externos (mercadopago) que validan firma propia en vez de
JWT.

Para mas detalle: [docs/architecture.md](docs/architecture.md),
[docs/openapi.yaml](docs/openapi.yaml) y el schema en
[graph/schema.graphqls](graph/schema.graphqls).

## Stack

- Go 1.23
- GraphQL (gqlgen)
- Postgres 16 + TimescaleDB para series temporales
- sqlc para queries tipadas
- pgx como driver
- chi para routing
- JWT HS256 para auth
- prometheus/client_golang para metricas
- Docker + GHCR para los builds
- GitHub Actions para CI

## Correr local

Necesitas:

- Go 1.23 o superior
- Docker (para la base timescale)
- make (opcional pero ayuda)

Primer arranque:

```bash
cp .env.example .env
make db-up
make run
```

El server queda en `http://localhost:8080`. Probalo:

```bash
curl -s http://localhost:8080/healthz | jq
```

Deberias ver algo asi:

```json
{ "status": "ok", "timestamp": "2026-11-06T22:30:00Z" }
```

Para ver las metricas:

```bash
curl -s http://localhost:8080/metrics | head -20
```

## Variables de entorno

Todas tienen default razonable salvo `DATABASE_URL` que es obligatoria.

| Variable             | Default                                                     | Para que                                        |
| -------------------- | ----------------------------------------------------------- | ----------------------------------------------- |
| `PORT`               | `8080`                                                      | puerto del HTTP server                          |
| `DATABASE_URL`       | (requerido)                                                 | conexion a postgres                             |
| `LOG_LEVEL`          | `info`                                                      | nivel de logs                                   |
| `JWT_SECRET`         | `dev-secret-change-me`                                      | firma de tokens                                 |
| `READ_TIMEOUT`       | `15s`                                                       | timeout de lectura HTTP                         |
| `WRITE_TIMEOUT`      | `15s`                                                       | timeout de escritura HTTP                       |
| `ENV`                | `development`                                               | prende/apaga modo debug                         |
| `MP_WEBHOOK_SECRET`  | (vacio)                                                     | secret del webhook de Mercado Pago              |

## Comandos de Make

- `make db-up`: levanta postgres + timescaledb en docker
- `make db-down`: baja la db
- `make run`: corre el server
- `make test`: corre tests con race
- `make lint`: corre golangci-lint
- `make fmt`: formatea codigo
- `make openapi`: valida docs/openapi.yaml

## Estructura

```
cmd/server/         main del proceso (serve, worker, migrate subcmds)
internal/config/    carga de environment
internal/health/    healthcheck handler
internal/auth/      jwt + middleware
internal/db/        pool pgx y queries generadas por sqlc
internal/weights/   dominio de peso (conversiones + service)
internal/txns/      dominio de plata (transactions + categorias)
internal/anomaly/   deteccion por z-score
internal/alerts/    worker y hypertable de alertas
internal/webhooks/  handlers de integraciones (mercadopago, etc)
internal/metrics/   contadores e histogramas prometheus
graph/              schema graphql y resolvers
db/                 migrations + queries sqlc
docs/               openapi + arquitectura
```

## Autenticacion

La api usa JWT con HS256. El secret se configura via `JWT_SECRET` (en prod,
que sea algo random de al menos 32 bytes, nunca el default).

### Flow basico

1. El cliente llama al endpoint de login (todavia no esta expuesto, lo armo
   junto con el schema de auth en la proxima tanda) con email + password.
2. El server verifica el hash y devuelve un token firmado con TTL de 12hs.
3. El cliente guarda el token y lo manda en cada request como
   `Authorization: Bearer <token>`.
4. El middleware valida la firma, el issuer y la expiracion, extrae el
   `sub` como UUID y lo mete en el context de la request.
5. Los resolvers de GraphQL protegidos con `@auth` sacan el user del
   context y filtran sus datos. Si no hay user, devuelven
   `UNAUTHENTICATED`.

### Que valida el middleware

- Header `Authorization` presente y con prefijo `Bearer ` (case-insensitive,
  hay varios clientes que mandan todo en minuscula).
- Algoritmo de firma: solo `HS256`. Un token con `alg=none` se rechaza
  antes de mirar los claims (el truco clasico de los primeros 2010s).
- `iss` tiene que ser `pulso-api`.
- `exp` no vencido.
- `sub` tiene que ser un UUID parseable.

Cuando algo falla el server responde `401` con un json chico:

```json
{ "error": "auth: token expirado", "code": "expired_token" }
```

El campo `code` es lo que mira el cliente para decidir si refrescar o
mandar al usuario de nuevo al login. Posibles: `invalid_token`,
`expired_token`, `wrong_issuer`, `bad_subject`.

### Variantes del middleware

- `auth.Middleware(signer)`: obligatorio. Si falla el token, corta con 401
  y no sigue. Lo uso en `/api/*`.
- `auth.Optional(signer)`: opcional. Si el token es valido, mete el user
  en el context. Si no, deja pasar sin user. Util para GraphQL donde
  algunas queries (como `ping`) son publicas.

El guard a nivel resolver es `graph.RequireUser(ctx)`, que devuelve el
user id o `ErrUnauthenticated`. En resolvers que ya son 100% protegidos
(porque el router ya aplico `Middleware`), igual lo llamo por defensa en
profundidad.

### Probarlo a mano

Hay una ruta chica `/api/me` que devuelve el user del token. Sirve de
smoke test:

```bash
TOKEN="pega-tu-token-aca"

curl -s http://localhost:8080/api/me \
  -H "Authorization: Bearer $TOKEN"
# -> {"user_id":"..."}
```

## Modelo financiero

Pulso guarda plata como una serie temporal igual que peso. La idea es
poder graficar "cuanto gaste en comida este mes", "cuanto me entro de
sueldo en el trimestre", "como evolucionan mis gastos fijos", sin tener
que inventar una tabla aparte para cada dashboard.

### Entidades

Hay dos tablas en el modulo plata:

- `categories`: agrupadores de gastos e ingresos. Cada una tiene un
  `kind` (`expense`, `income`, `transfer`) y pueden ser globales (las
  trae la migration, `user_id = null`) o propias del user. Un user puede
  renombrar, archivar y crear las que quiera, pero no puede editar las
  globales.
- `transactions`: cada movimiento. Es hypertable sobre `occurred_at`
  (chunk de 7 dias) con FK a `categories`. Guarda `amount` como
  `numeric(14,2)` y una `currency` de 3 letras (ISO 4217). Si viene de
  una integracion, `external_source` dice de donde (`mercadopago`,
  `cuentaDNI`, etc) y `external_ref` es el id del evento para dedup.

### Reglas de signo

- `expense` siempre guarda el amount en **negativo**.
- `income` siempre guarda el amount en **positivo**.
- `transfer` queda como venga (depende del lado de la transferencia).

El resolver `createTransaction` normaliza antes de insertar, asi los
`SUM()` por `kind` funcionan directo sin tener que hacer `ABS()` ni
casos especiales.

### Monedas

El server no convierte. Guardo cada transaction en la moneda original y
la conversion a ARS la hago en el cliente con los tipos de cambio
cacheados. Si el cliente cambia el tipo de cambio (porque en Argentina
hay varios) no quiero reescribir la historia.

### Deduplicacion

Cuando una transaction viene de una integracion (Mercado Pago, por
ejemplo), el par `(user_id, external_source, external_ref)` tiene un
unique index parcial. Si el webhook re-dispara el mismo payment id,
el insert falla con `23505` y el handler devuelve 200 igual.

### Webhook de Mercado Pago

MP manda eventos `POST /webhooks/mercadopago` con un header
`x-signature` firmado con HMAC-SHA256 sobre el string:

```
id:<data.id>;request-id:<x-request-id>;ts:<timestamp>;
```

El secret se configura en `MP_WEBHOOK_SECRET`. El handler:

- Rechaza requests fuera de una ventana de 5 minutos (replay).
- Deduplica por `x-request-id` en memoria 10 minutos.
- Encola el evento validado en un `EventSink`. En dev el sink es un
  noop. En prod va a un worker que trae el detalle, infiere la
  categoria y escribe el row usando el unique index para dedup.

## Deteccion de anomalias

El modulo `internal/anomaly` arma una ventana rodante de 14 pesos
previos por user y calcula z-score. Si abs(z) > 3 y hay al menos 7
puntos de historia, dispara una alerta que se guarda en la hypertable
`alerts` y se expone por graphql. Las alertas se muestran en el
dashboard web (widget de anomalias) y en el home del mobile.

El worker corre asincrono: el resolver de `createWeight` manda el
weight_id al channel y vuelve. Esto es a proposito, no quiero que un
error del detector rompa el write principal.

## Observabilidad

`/metrics` expone en formato prometheus. Metricas principales:

- `pulso_http_requests_total{route,method,status}`
- `pulso_http_request_duration_seconds{route,method}` (histograma)
- `pulso_db_queries_total{op}`
- `pulso_alerts_emitted_total{kind}`
- `pulso_webhooks_events_total{provider,result}`

Hay un dashboard de grafana en `docs/dashboards/` (todavia por subir,
queda para la proxima iteracion).

## Docker y despliegue

Imagen multi-arch publicada en GHCR en cada push a main:

```
ghcr.io/matiashernanserrato/pulso-api:latest
ghcr.io/matiashernanserrato/pulso-api:main
ghcr.io/matiashernanserrato/pulso-api:sha-<corto>
```

Correr:

```bash
docker run -d --name pulso-api \
  -p 8080:8080 \
  -e DATABASE_URL="postgres://..." \
  -e JWT_SECRET="..." \
  ghcr.io/matiashernanserrato/pulso-api:latest
```

El target de deploy real es fly.io, ver seccion de abajo.

## Fly.io

Config en `fly.toml`. Dos procesos:

- `app`: el server HTTP, escalable, 512 MB.
- `worker`: loop cada 60s que corre deteccion de anomalias y mas
  adelante consumira la outbox de Mercado Pago. 256 MB alcanza.

El binario es el mismo, cambia el subcomando. `main.go` ruteea por
`os.Args[1]` entre `serve`, `worker`, `migrate` y `version`. Sin
argumentos default a `serve`, asi el docker-compose local sigue
funcionando igual.

### Deploy manual

```bash
fly auth login
fly apps create pulso-api            # la primera vez
fly volumes create pulso_data --region gru --size 1
fly secrets set \
  DATABASE_URL="postgres://user:pass@..." \
  JWT_SECRET="$(openssl rand -base64 32)" \
  MP_WEBHOOK_SECRET="$(openssl rand -base64 32)"

fly deploy \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg VERSION="$(git describe --tags --always)" \
  --build-arg BUILD_DATE="$(date -u +%FT%TZ)"
```

El `release_command` de fly corre `pulso-api migrate up` antes de
rotar las maquinas, asi nunca llega trafico a una version con la db
desfasada.

### Healthcheck

Fly le pega a `/healthz` cada 15s via `http_checks` del `[[services]]`.
Si falla 3 veces seguidas rolea la maquina. El handler devuelve 200
mientras el pool de pgx responda un `Ping` en menos de 500ms.

### Autostop

`auto_stop_machines = "stop"` apaga la maquina cuando no hay trafico,
pero `min_machines_running = 1` me asegura que siempre quede una
despierta. En un side project con 3 usuarios esto es la diferencia
entre pagar 2 USD y 20 USD por mes.

## Por que Go y no Node

Porque ya tengo 9 años de Node y queria probar otra cosa. Ademas para
series temporales el ecosistema de Go esta mas maduro de lo que
parece: pgx + sqlc es una experiencia muy piola, los tipos no mienten
y no tengo que pelearme con ORMs que se creen que saben mas que yo.
