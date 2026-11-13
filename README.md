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

## Estructura

```
cmd/server/      main del proceso
cmd/migrate/     corre migraciones contra la db
internal/config/ carga de environment
internal/health/ healthcheck handler
internal/auth/   jwt + middleware
internal/db/     pool pgx y queries generadas por sqlc
internal/weights/ dominio de peso (conversiones + service)
internal/webhooks/ handlers HTTP de integraciones (mercadopago, etc)
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
  globales: si no le gustan, se archiva la copia global para su user
  (cuando este el mecanismo de fork) y arma la propia.
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
casos especiales. Si en algun momento aparecen "reintegros" (expense con
monto positivo, porque te devolvieron plata) los guardo como `income`
categorizado como "Reintegro", no como expense positivo. Mas facil que
discutir con los charts.

### Monedas

El server no convierte. Guardo cada transaction en la moneda original y
la conversion a ARS la hago en el cliente con los tipos de cambio
cacheados. Esto es a proposito: si el cliente cambia el tipo de cambio
(porque en Argentina hay varios) no quiero reescribir la historia.

Hay ganas de agregar una tabla `fx_rates` para cachear cotizaciones en
el server y exponerlas por GraphQL, pero no es parte del alcance del
portfolio demo.

### Deduplicacion

Cuando una transaction viene de una integracion (Mercado Pago, por
ejemplo), el par `(user_id, external_source, external_ref)` tiene un
unique index parcial. Si el webhook re-dispara el mismo payment id,
el insert falla con `23505` y el handler devuelve 200 igual. Ver abajo.

### Webhook de Mercado Pago

MP manda eventos `POST /webhooks/mercadopago` con un header
`x-signature` firmado con HMAC-SHA256 sobre el string:

```
id:<data.id>;request-id:<x-request-id>;ts:<timestamp>;
```

El secret se configura en `MP_WEBHOOK_SECRET` (el que figura en el
panel de MP al crear la integracion). Si no esta seteado, el endpoint
responde 503 y loguea un warning. Fuera de dev no tiene sentido tenerlo
apagado.

El handler ademas:

- Rechaza requests fuera de una ventana de 5 minutos (replay).
- Deduplica por `x-request-id` en memoria 10 minutos (MP reintenta el
  mismo evento si no respondemos 200 rapido).
- Encola el evento validado en un `EventSink`. En dev el sink es un
  noop que solo loguea. En prod va a ir a un worker que llama a la API
  de MP con el `data.id`, trae el detalle del payment, infiere la
  categoria y escribe el row en `transactions` usando los campos
  `external_source = "mercadopago"` + `external_ref = <payment_id>`
  para pegarle al unique index de dedup.

### Categorizacion

Por ahora la categoria viaja explicita en el input del mutation. Mas
adelante quiero meter un clasificador bobo por texto (expresiones
regulares sobre `note` y `description` del detalle de MP) que sugiera
la mas probable: "uber" -> Transporte, "disco" -> Super, etc. No es ML,
es un switch gigante y listo.

Si el user no manda categoria, la transaction queda con `category_id =
null`. Cuando esto pasa el dashboard las junta como "Sin categoria" y
el cliente ofrece un bulk-update. Es feo pero prefiero eso a adivinar
mal y ensuciar las series de tiempo.

### Por que no usar `accounts`

Varias apps del rubro modelan "cuentas" (efectivo, tarjeta, caja de
ahorro) como tablas separadas y despues tienen una visibilidad lio con
las transferencias entre cuentas. Pulso arranca con un unico "wallet"
implicito por user. Si en algun momento aparecen multiples cuentas:

- Se agrega tabla `accounts (id, user_id, name, kind, currency)`.
- `transactions` gana un `account_id NOT NULL`.
- La transferencia pasa a ser un par de `transactions` linkeadas por un
  `transfer_id` (asi el `SUM()` por account sigue dando bien).

Mientras no haya varias cuentas no vale la pena, agrega complejidad sin
beneficio. Si lo necesitas antes de que lo mueva, abri un issue.

## Por que Go y no Node

Porque ya tengo 9 años de Node y queria probar otra cosa. Ademas para series
temporales el ecosistema de Go esta mas maduro de lo que parece.
