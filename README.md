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
cmd/migrate/     corre migraciones contra la db
internal/config/ carga de environment
internal/health/ healthcheck handler
internal/auth/   jwt + middleware
internal/db/     pool pgx y queries generadas por sqlc
internal/weights/ dominio de peso (conversiones + service)
graph/           schema graphql y resolvers
db/              migrations + queries sqlc
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
# generar un token con el helper que hay en los tests, o desde codigo:
TOKEN="pega-tu-token-aca"

curl -s http://localhost:8080/api/me \
  -H "Authorization: Bearer $TOKEN"
# -> {"user_id":"..."}
```

## Por que Go y no Node

Porque ya tengo 9 años de Node y queria probar otra cosa. Ademas para series
temporales el ecosistema de Go esta mas maduro de lo que parece.
