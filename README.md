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

## Estado

Arrancando. Todavia no hay nada util corriendo.

## Correr local

Todavia no, falta docker-compose. Proximos dias.

## Por que Go y no Node

Porque ya tengo 9 años de Node y queria probar otra cosa. Ademas para series
temporales el ecosistema de Go esta mas maduro de lo que parece.
