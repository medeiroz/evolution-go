# Deploy no Coolify

Fork **local-mode** do `evolution-foundation/evolution-go` v0.7.2:

- **Sem telemetria** — `pkg/telemetry` é no-op (upstream fazia `POST` da rota de toda request pra `log.evolution-api.com`).
- **Sem license server** — `pkg/core` foi **removido por completo** (o `c0.go` de 21KB com activation + heartbeat + integridade virou um `core.go` de 56 linhas, inerte, zero rede). Nenhum `/v1/activate`, nenhum heartbeat, nenhum phone-home no binário.

Tudo o mais é o upstream 0.7.2 intacto (compila e sobe de verdade — diferente do fork `NathanAshford/evolution-go-custom`, que não trazia o entrypoint nem o servidor HTTP).

Postgres = **18-alpine** (última major, 18.4).

## Domínio

Este stack sobe em **`evolution.medeiroz.com`**:

1. Aponte o **DNS**: registro `A` de `evolution.medeiroz.com` → IP do servidor Coolify.
2. No Coolify, depois que a compose for lida: **Service `evolution-go` → Domains** → digite `evolution.medeiroz.com` (a magic var `SERVICE_FQDN_EVOLUTIONGO_8080` já diz ao proxy pra rotear a porta 8080). O Coolify emite o TLS via Let's Encrypt.
3. `PASSKEY_PUBLIC_URL` já está fixo em `https://evolution.medeiroz.com` na compose.

## Passos no Coolify

1. **New Resource → Docker Compose** apontando pra este repositório (branch `main`).
2. Coolify lê o `docker-compose.yaml` e faz build da imagem pelo `Dockerfile`.
3. As *magic vars* geram os segredos sozinhas na primeira subida:
   - `GLOBAL_API_KEY` ← `SERVICE_PASSWORD_64_APIKEY` (chave de 64 chars)
   - senha do Postgres ← `SERVICE_PASSWORD_PG`
4. Configure o domínio (seção acima) e **Deploy.** Pegue a `GLOBAL_API_KEY` gerada na aba *Environment Variables* — é o header `apikey` de toda request.

## Testar local antes (mesma compose)

```bash
# .env local (gitignored) com valores concretos p/ as magic vars:
printf 'SERVICE_PASSWORD_PG=%s\nSERVICE_PASSWORD_64_APIKEY=%s\n' \
  "$(openssl rand -hex 16)" "$(openssl rand -hex 32)" > .env
docker compose up -d --build
docker compose ps                 # ambos devem ficar (healthy)
docker compose logs -f evolution-go
docker compose down               # (down -v apaga os volumes/dados)
```

## Notas

- **Postgres é interno** — sem porta publicada. Só o app acessa pela rede do stack.
- **Build precisa de RAM** (compile CGO do `whatsmeow`, ~30s). Em VPS pequena, garanta swap ou a build pode dar OOM.
- Manager UI em `https://evolution.medeiroz.com/manager`; API na raiz. Healthcheck bate em `/manager`.
- Dados persistem em `postgres_data` (montado em `/var/lib/postgresql`, path do PG18), `evolution_data`, `evolution_logs`.
- Log não-fatal de NATS no boot é esperado (transport de eventos opcional, não configurado).

## Atualizar do upstream depois

Este repo tem histórico próprio (1 commit), então não dá `rebase` direto no upstream.
Para subir a uma nova versão, reaplique as 2 mudanças por cima da tag nova:

```bash
git clone https://github.com/evolution-foundation/evolution-go.git up && cd up
git checkout <nova-tag>
# 1) telemetria: zerar pkg/telemetry/telemetry.go (middleware/SendTelemetry viram no-op)
# 2) licença: substituir pkg/core/* pelo core.go inerte (stub dos símbolos que o main.go usa)
```

Depois copie a árvore resultante por cima deste repo, rode `docker compose up --build`
(ambos `healthy`), e confirme por grep que não sobrou `/v1/`, `evolution-api.com`
nem import de rede em `pkg/core`.
