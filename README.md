# pulso-api

Backend de Pulso, un tracker personal que mezcla salud y plata.
Idea: una sola API que me deje registrar peso, actividad, gastos e inversiones,
y sacar graficos decentes sin tener que usar 4 apps distintas.

Esto es un side project, lo uso para probar cosas que en el laburo no me dejan
(TimescaleDB, GraphQL federado, Go con generics bien usados).

## Stack

- Go 1.23
- GraphQL (gqlgen)
- Postgres 16 + TimescaleDB para series temporales
- sqlc para queries tipadas
- pgx como driver
- chi para routing
- JWT para auth

## Estado

Arrancando. Ya hay /healthz respondiendo y el loader de config. Proximo paso
es meter gqlgen y las primeras tablas.

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

## Variables de entorno

Todas tienen default razonable salvo `DATABASE_URL` que es obligatoria.

| Variable        | Default                                                     | Para que                    |
| --------------- | ----------------------------------------------------------- | --------------------------- |
| `PORT`          | `8080`                                                      | puerto del HTTP server      |
| `DATABASE_URL`  | (requerido)                                                 | conexion a postgres         |
| `LOG_LEVEL`     | `info`                                                      | nivel de logs               |
| `JWT_SECRET`    | `dev-secret-change-me`                                      | firma de tokens             |
| `READ_TIMEOUT`  | `15s`                                                       | timeout de lectura HTTP     |
| `WRITE_TIMEOUT` | `15s`                                                       | timeout de escritura HTTP   |
| `ENV`           | `development`                                               | prende/apaga modo debug     |

## Comandos de Make

- `make db-up`: levanta postgres + timescaledb en docker
- `make db-down`: baja la db
- `make run`: corre el server
- `make test`: corre tests con race
- `make lint`: corre golangci-lint
- `make fmt`: formatea codigo

## Estructura

```
cmd/server/      main del proceso
internal/config/ carga de environment
internal/health/ healthcheck handler
```

Mas adelante, cuando sume gqlgen, va a aparecer `internal/graph/`, y cuando
meta sqlc algo asi como `internal/db/`.

## Por que Go y no Node

Porque ya tengo 9 años de Node y queria probar otra cosa. Ademas para series
temporales el ecosistema de Go esta mas maduro de lo que parece.
