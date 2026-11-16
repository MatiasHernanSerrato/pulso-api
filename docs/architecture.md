# Arquitectura de pulso-api

Este documento explica como esta armado pulso-api por dentro: que paquetes
hay, por donde pasa un request, y cuales son las decisiones que capaz no se
ven mirando un archivo suelto.

Es un side project, no una API de prod. Lo escribo pensando en que vuelvo
en 6 meses sin acordarme nada y necesito reubicarme rapido, o en que algun
reclutador quiere entender como pienso cosas.

## Idea general

Pulso trackea dos mundos que normalmente viven en apps separadas:

- Salud: peso, body fat, mas adelante actividad y sueño.
- Plata: gastos, ingresos, inversiones (via otra API nuestra).

Y les mete un detector de anomalies simple arriba para avisar cuando algo
se va de la vaina. El punto dulce es la combinacion: ver que en la semana
que arranque el gym tambien baje el gasto en delivery.

## Stack

- Go 1.23 (toolchain 1.24.7 en el go.mod porque mi mac tiene esa)
- Postgres 16 + TimescaleDB (hypertables sobre weights y alerts)
- sqlc para queries tipadas, pgx como driver
- GraphQL via gqlgen (schema en graph/schema.graphqls)
- chi para routing HTTP de todo lo que no es GraphQL
- JWT para auth con HMAC-SHA256
- golangci-lint + go vet + race detector en CI

## Capas

Vista por donde cae un request:

```
HTTP (chi router)
  |
  +-- middlewares: requestID, logger, recover, timeout, auth (en /api)
  |
  +-- /healthz                       -> internal/health
  +-- /webhooks/mercadopago         -> internal/webhooks
  +-- /api/graphql (pending)        -> graph/* (resolvers)
  |                                      |
  |                                      +-- weights  -> internal/weights.Service -> sqlc
  |                                      +-- txns     -> internal/txns.Service    -> in-memory por ahora
  +-- /api/anomaly/weights          -> internal/anomaly (http.Handler)
                                            |
                                            +-- Loader que consulta internal/weights
```

El principio general: los handlers y resolvers no tocan la db ni hacen
validacion de dominio, solo adaptan el input y delegan al service. Los
servicios no conocen HTTP ni GraphQL, se pueden testear con contexto y nada.

## Paquetes

### cmd/
- `cmd/server`: entrypoint HTTP. Carga config, arma el pool, cablea routers
  y middlewares, apaga en SIGTERM con grace period.
- `cmd/migrate`: corre las migrations antes de levantar el server. Lo tengo
  separado para poder correrlo como job de deploy sin ensuciar el binario
  del server.

### internal/
- `config`: envconfig. Validacion de los campos obligatorios al arrancar,
  no mas tarde. Si falta `JWT_SECRET` revienta el proceso, no un endpoint
  random a las 3 am.
- `auth`: signer JWT + middleware chi + helpers de context. Un solo signer
  por proceso, el secret se rota via deploy.
- `db`: pool de pgx con defaults razonables para fly.io, y runner de migrations.
- `db/sqlcgen`: output generado por sqlc. No se edita a mano, se regenera
  con `make graphql` (nombre poco feliz, deberia ser `gen`).
- `weights`: service layer sobre la tabla weights. Valida rangos, mapea
  entre pgtype.Numeric y float64 de forma consistente.
- `txns`: service layer sobre transactions y categorias. Por ahora el
  storage es en memoria; cuando pase a sqlc solo cambia este paquete.
- `anomaly`: detector z-score con ventana movil. Puro, sin IO. Expone un
  http.Handler con un adapter (Loader) para traer la data.
- `alerts`: modelo + worker que empuja al store. El worker usa atomic
  counters para que `-race` no se queje y drena en shutdown.
- `webhooks`: handler y worker de Mercado Pago. El handler valida firma
  HMAC, encola el evento y responde 200 rapido.
- `health`: /healthz. Un solo archivo, nada sofisticado.

### graph/
- `schema.graphqls`: schema fuente. Es la unica source of truth del contrato.
- `resolver.go`: Resolver root + implementacion de weights (todavia en memoria).
- `transactions.go`: wrapper fino sobre `internal/txns.Service`.
- `directives.go`: implementacion de @auth, mas el helper `RequireUser`.
- `model/`: structs del schema a mano. Se van a regenerar con `gqlgen generate`.

### db/
- `migrations/`: numeradas, up y down. Compatibles con golang-migrate.
  Timescale se activa en la 0001.
- `queries/`: fuente sqlc. Un archivo por entidad.

## Decisiones

### Por que TimescaleDB y no una tabla normal

Weights y alerts crecen monotonicamente en tiempo y 95% de las queries son
por rango. Las hypertables chunkean por tiempo, asi las queries recientes
no tocan particiones viejas. Chunks de 1 dia para weights (mediciones
diarias), 7 dias para alerts (menos volumen).

### Por que GraphQL

Portfolio, honestamente. Tambien porque la app mobile y la web quieren
campos distintos por pantalla y no quiero hacer 15 endpoints REST que no
ensamblan bien. GraphQL me deja lo mismo con un solo contrato.

### Por que un service layer encima de sqlc

Tres razones:

1. sqlc genera queries, no reglas. La validacion ("un weight < 500 kg")
   la quiero cerca del codigo, no solo como CHECK constraint que devuelve
   un error de pg crudo.
2. Tipos del dominio (Weight con float64) son distintos a los de sqlc
   (pgtype.Numeric). El mapping vive en una sola capa.
3. Cuando necesitemos eventos (publicar a alerts worker al insertar un
   peso raro) el service es el lugar natural.

### Por que in-memory para txns todavia

Priorizamos sacar el flow completo end-to-end antes de clavar los schemas
de transactions. Cuando esta cerrado, el refactor es local al paquete.

### Auth

JWT con HMAC-SHA256. Access tokens cortos, refresh todavia no (va mas
adelante). El middleware lee el header Authorization, verifica, mete el
uuid del user en ctx. Los resolvers llaman `RequireUser(ctx)` al toque.

Los webhooks externos NO usan JWT: tienen su propia firma. El handler de
MP es el ejemplo: valida HMAC con un secret por app.

### Anomaly detection

Z-score con ventana movil de 28 dias, cortes en 2.0 / 2.5 / 3.5. Elecciones:

- Z-score por simple. Isolation forest o DBSCAN son overkill para un
  usuario con una medicion por dia.
- Ventana de 28 dias porque menos agarra ruido de ciclo semanal y mas no
  reacciona a cambios de estilo de vida reales (dieta, lesion).
- El detector no mira el futuro: para la lectura en t solo usa (t - 28d, t).
  Asi la salida es estable y no cambia al cargar un peso nuevo.

Limitacion conocida: no detecta drift lento. Para eso hace falta CUSUM o
parecido, queda para mas adelante.

### Alerts: dedupe

El unique index parcial de alerts garantiza que no haya dos alerts abiertas
con la misma dedupe_key por user. Las resolved o dismissed vuelven a poder
dispararse, lo cual es el comportamiento que queremos: si el usuario resolvio
y despues vuelve a aparecer, es nueva info.

### Mercado Pago webhook

El handler valida firma HMAC con el secret y responde 200 rapido para
no gatillar retries de MP. El procesamiento real es async (worker con
backoff). Si el pipeline esta roto, el handler aun responde 200 asi MP
no inunda con retries; el evento queda en cola y lo manejamos por nuestro
lado (log, metricas, DLQ cuando tengamos).

## Que falta

- Cablear gqlgen generate y matar los stubs manuales de QueryResolver/MutationResolver.
- Mover weights a service real con sqlc (falta cablear el resolver contra `internal/weights.Service`).
- Alerts worker contra postgres (el Store real).
- OpenTelemetry (spans por request + export a honeycomb o parecido).
- Rate limit en /webhooks/mercadopago y en /api/.
- Docker build + push to ghcr en CI, deploy a fly.io.
