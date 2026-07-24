# Evo-Conversations — Ingestão de Mensagens — Plano de Implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Persistir todas as mensagens de todas as instâncias do evolution-go numa database Postgres nova (`evogo_analytics`), com mídia no Cloudflare R2, via um consumer Benthos config-only rodando no Coolify.

**Architecture:** evolution-go publica eventos no RabbitMQ (filas `message`/`sendmessage`) e sobe mídia no R2 (`MINIO_ENABLED`). Um container Benthos (Redpanda Connect) na rede `coolify` consome as filas, extrai campos + texto, e faz `INSERT ... ON CONFLICT` na tabela `raw_messages`. Fase B (IA) fica fora.

**Tech Stack:** Redpanda Connect (Benthos) YAML, PostgreSQL 18, Cloudflare R2 (S3), Docker Compose, Coolify v4.1.2, coolify CLI.

**Spec de referência:** `docs/superpowers/specs/2026-07-24-evo-conversations-ingestion-design.md`

## Global Constraints

- Segredos (`AMQP_URL`, `ANALYTICS_PG_DSN`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`) vivem em **envs do Coolify**, nunca no git. No compose/config só `${...}`.
- Todos os recursos Coolify são endereçados por **UUID** (não ID numérico).
- Context coolify default: `medeiroz`. Comandos `coolify …` já apontam pra ele.
- PK da tabela: **`(instance_id, message_id)`** (o `Info.ID` do WhatsApp não é único global).
- `raw` gravado como **texto JSON** (`format_json()`), não objeto (lib/pq rejeita map).
- Ligar `MINIO_*` é **atômico com `CONNECT_ON_STARTUP=true`** e todas as 4 envs obrigatórias (senão o app panica no boot).
- Postgres gerenciado acessível de fora em `157.151.17.255:65344` (Cloudflare bloqueia o domínio — usar IP de origem).
- Legenda dos passos: **[VOCÊ]** = ação humana (UI/credenciais); **[AGENTE]** = automatizável via CLI.

## Referência de infra (UUIDs/hosts)

| Recurso | Valor |
|---------|-------|
| App `evolution-go` | `v6avb69d6saxp5whzm5bgyyt` |
| Projeto `Evolution Go` | `huos8mn899kb3sq61aoben8k` (env `production`) |
| Servidor (OCI) | `nwgkccgok8wo4kcsooogkg8w` |
| Postgres gerenciado | `rmuon5lug96hpx0uerincy3h` (host interno `:5432`; externo `157.151.17.255:65344`) |
| RabbitMQ service | `m9jkqoqkkc8q8ijslc3npmf2` (host interno `rabbitmq-m9jkqoqkkc8q8ijslc3npmf2:5672`) |
| Bucket R2 | `evolution-go` (objeto `evolution-go-medias/<id><ext>`) |
| Rede Docker | `coolify` (predefined network) |

---

## Task 0: Pré-requisitos R2 + inserir segredos no Coolify — **[VOCÊ]**

O token do R2 é inserido **por você direto no Coolify** (o agente nunca vê as credenciais).

**Files:** nenhum (Cloudflare + envs no Coolify).

**Interfaces:**
- Produces: envs `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY` já criadas no app `evolution-go` (uuid `v6avb69d6saxp5whzm5bgyyt`) — consumidas pelo `${...}` no compose (Task 1).

- [ ] **Step 1 [VOCÊ]: Criar token de API do R2 (S3) escopado ao bucket `evolution-go`**

Cloudflare → R2 → **Manage R2 API Tokens** → *Create API token* → **Object Read & Write**, escopo **só o bucket `evolution-go`**. Anote **Access Key ID** e **Secret Access Key** (o secret só aparece uma vez). O endpoint S3 é `https://<ACCOUNT_ID>.r2.cloudflarestorage.com`.

- [ ] **Step 2 [VOCÊ]: Inserir os 3 envs secretos no app `evolution-go` (Coolify UI)**

App evolution-go → **Environment Variables** → criar:
- `MINIO_ENDPOINT` = `<ACCOUNT_ID>.r2.cloudflarestorage.com` (host, sem `https://`)
- `MINIO_ACCESS_KEY` = Access Key ID do R2
- `MINIO_SECRET_KEY` = Secret Access Key do R2

Não fazer redeploy ainda (a Task 1 faz, junto com o compose). Avisar o agente quando os 3 estiverem criados.

---

## Task 1: evolution-go — mídia no R2 + reconexão automática

Liga `MINIO_ENABLED` (mídia → R2) e `CONNECT_ON_STARTUP` (reconecta após redeploy), atômico.

**Files:**
- Modify: `docker-compose.yaml` (env do serviço `evolution-go`)

**Interfaces:**
- Consumes: envs `MINIO_ENDPOINT`/`MINIO_ACCESS_KEY`/`MINIO_SECRET_KEY` já criadas por você (Task 0).
- Produces: payloads AMQP com `data.Message.mediaUrl` (sem base64) para mídia recebida.

- [ ] **Step 1 [AGENTE]: Confirmar que os 3 envs MinIO já existem no app** (criados por você na Task 0)

```bash
coolify app env list v6avb69d6saxp5whzm5bgyyt --format json | python3 -c "import sys,json;need={'MINIO_ENDPOINT','MINIO_ACCESS_KEY','MINIO_SECRET_KEY'};ks={e['key'] for e in json.load(sys.stdin)};print('OK' if need<=ks else 'FALTANDO: '+str(need-ks))"
```
Expected: `OK`. Se `FALTANDO`, pedir pra você criar antes de seguir (senão o app panica no boot).

- [ ] **Step 2 [AGENTE]: Editar `docker-compose.yaml`** — trocar os toggles e adicionar as refs MinIO

Localizar em `services.evolution-go.environment`:
```yaml
      CONNECT_ON_STARTUP: "false"
```
trocar por:
```yaml
      CONNECT_ON_STARTUP: "true"
```
Localizar:
```yaml
      MINIO_ENABLED: "false"
```
trocar por:
```yaml
      MINIO_ENABLED: "true"
      MINIO_ENDPOINT: "${MINIO_ENDPOINT}"
      MINIO_ACCESS_KEY: "${MINIO_ACCESS_KEY}"
      MINIO_SECRET_KEY: "${MINIO_SECRET_KEY}"
      MINIO_BUCKET: "evolution-go"
      MINIO_USE_SSL: "true"
      MINIO_REGION: "auto"
```

- [ ] **Step 3 [AGENTE]: Commit + push**

```bash
git add docker-compose.yaml
git commit -m "feat(media): mídia via R2 (MINIO_ENABLED) + CONNECT_ON_STARTUP

Claude-Session: https://claude.ai/code/session_01CgafPNy4DkTzEXFCtUQZZK"
git push origin main
```
Expected: push ok para `main`.

- [ ] **Step 4 [AGENTE]: Aguardar o deploy automático (GitHub App trigger)**

O push do Step 3 dispara o deploy pelo **GitHub App** do Coolify (não há pipeline GitHub Actions). Aguardar `finished`; se não disparar em ~1min, forçar `coolify deploy uuid v6avb69d6saxp5whzm5bgyyt`.

```bash
# aguardar: coolify app deployments list v6avb69d6saxp5whzm5bgyyt --format json | jq '.[0].status'  → "finished"
```

- [ ] **Step 5 [AGENTE]: Validar que o app subiu SEM panic de config**

```bash
coolify app logs v6avb69d6saxp5whzm5bgyyt -n 120 | grep -iE "panic|fatal|MINIO|minio|CONFIG"
```
Expected: sem `panic`/`fatal`. Se aparecer `panic` mencionando `MINIO_*`, alguma env ficou vazia → revisar Step 1.

- [ ] **Step 6 [AGENTE]: Validar reconexão automática das instâncias**

```bash
KEY=$(coolify app env list v6avb69d6saxp5whzm5bgyyt -s --format json | python3 -c "import sys,json;print(next(e['real_value'] for e in json.load(sys.stdin) if e['key']=='SERVICE_PASSWORD_64_APIKEY'))")
sleep 20
curl -s -H "apikey: $KEY" https://evolution.medeiroz.com/instance/all | python3 -c "import sys,json;[print(i['name'],i['connected']) for i in json.load(sys.stdin)]"
```
Expected: ambas `connected: True` **sem** reconexão manual (prova do `CONNECT_ON_STARTUP`).

- [ ] **Step 7 [VOCÊ]: Enviar 1 mídia de teste** (um áudio ou imagem) por uma instância.

- [ ] **Step 8 [AGENTE]: Validar mídia no R2 (payload com `mediaUrl`, sem `base64`)**

```bash
U=YIGIxNF1JlwSnfJC; P=koYA1W5PyGiwsn0qen5voaDNpxOYX5wh; B=https://rabbitmq-evolution.medeiroz.com
curl -s -u "$U:$P" -H "content-type: application/json" -X POST "$B/api/queues/%2F/message/get" \
  -d '{"count":10,"ackmode":"ack_requeue_true","encoding":"auto","truncate":50000}' \
  | python3 -c "
import sys,json
for m in json.load(sys.stdin):
    try: d=json.loads(m['payload'])['data']
    except: continue
    msg=d.get('Message',{})
    if any(k.endswith('Message') and 'Text' not in k for k in msg) or 'mediaUrl' in msg:
        print('type:',d['Info']['Type'],'| mediaUrl?', 'mediaUrl' in msg, '| base64?', 'base64' in msg, '| mimetype:', msg.get('mimetype'))
"
```
Expected: mídia **nova** com `mediaUrl? True` e `base64? False`. (As antigas continuam base64 — ok.)

---

## Task 2: Database analítica `evogo_analytics` + tabela `raw_messages`

**Files:**
- Create: `~/code/evo-conversations/migrations/001_init.sql`

**Interfaces:**
- Produces: database `evogo_analytics`, tabela `raw_messages` com PK `(instance_id, message_id)`.

- [ ] **Step 1 [AGENTE]: Criar o repo local e a migration**

```bash
mkdir -p ~/code/evo-conversations/migrations
```
Criar `~/code/evo-conversations/migrations/001_init.sql`:
```sql
-- Conectar ao DB `postgres` para criar a database (CREATE DATABASE não roda em transação):
CREATE DATABASE evogo_analytics;

-- Depois conectar em `evogo_analytics` e rodar o restante:
CREATE TABLE IF NOT EXISTS raw_messages (
  message_id    text NOT NULL,
  instance_id   text NOT NULL,
  instance_name text,
  direction     text NOT NULL,
  chat_jid      text,
  sender_jid    text,
  push_name     text,
  is_group      boolean,
  msg_type      text,
  msg_ts        timestamptz,
  body          text,
  media_url     text,
  media_path    text,
  mimetype      text,
  event         text,
  raw           jsonb NOT NULL,
  ingested_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (instance_id, message_id)
);
CREATE INDEX IF NOT EXISTS raw_messages_instance_ts_idx ON raw_messages (instance_id, msg_ts);
CREATE INDEX IF NOT EXISTS raw_messages_chat_ts_idx     ON raw_messages (chat_jid, msg_ts);
```

- [ ] **Step 2 [AGENTE]: Aplicar a migration via psql pela porta pública**

```bash
PGPW=$(coolify database env list rmuon5lug96hpx0uerincy3h -s --format json 2>/dev/null | python3 -c "import sys,json;print(next(e['real_value'] for e in json.load(sys.stdin) if e['key'] in ('SERVICE_PASSWORD_PG','POSTGRES_PASSWORD','MANAGED_PG_PASSWORD')))" 2>/dev/null)
# fallback: pegar da env do app
[ -z "$PGPW" ] && PGPW=$(coolify app env list v6avb69d6saxp5whzm5bgyyt -s --format json | python3 -c "import sys,json;print(next(e['real_value'] for e in json.load(sys.stdin) if e['key']=='MANAGED_PG_PASSWORD'))")
HOST=157.151.17.255; PORT=65344
# 1) criar a database (conectando no db 'postgres')
psql "postgresql://postgres:$PGPW@$HOST:$PORT/postgres?sslmode=disable" -v ON_ERROR_STOP=1 -c "CREATE DATABASE evogo_analytics;" || echo "(já existe? ok)"
# 2) criar tabela+índices dentro de evogo_analytics (pula a linha do CREATE DATABASE)
tail -n +5 ~/code/evo-conversations/migrations/001_init.sql | psql "postgresql://postgres:$PGPW@$HOST:$PORT/evogo_analytics?sslmode=disable" -v ON_ERROR_STOP=1
```
Expected: `CREATE DATABASE`, `CREATE TABLE`, `CREATE INDEX` (2x). Se `psql` não existir localmente, usar `docker run --rm -i postgres:18-alpine psql ...`.

- [ ] **Step 3 [AGENTE]: Validar o schema**

```bash
psql "postgresql://postgres:$PGPW@157.151.17.255:65344/evogo_analytics?sslmode=disable" -c "\d raw_messages" -c "SELECT count(*) FROM raw_messages;"
```
Expected: colunas conforme migration, PK `(instance_id, message_id)`, count `0`.

---

## Task 3: Repo `evo-conversations` — Benthos config + Dockerfile + compose

Empacota o Benthos com a config embutida (mesma lógica de build-from-repo do evolution-go).

**Files:**
- Create: `~/code/evo-conversations/benthos.yaml`
- Create: `~/code/evo-conversations/Dockerfile`
- Create: `~/code/evo-conversations/docker-compose.yaml`
- Create: `~/code/evo-conversations/README.md`
- Create: `~/code/evo-conversations/.gitignore`

**Interfaces:**
- Consumes: filas `message`/`sendmessage`; tabela `raw_messages` (Task 2).
- Produces: imagem que consome AMQP → grava no Postgres; env necessárias `AMQP_URL`, `ANALYTICS_PG_DSN`.

- [ ] **Step 1 [AGENTE]: Criar `benthos.yaml`**

```yaml
input:
  broker:
    inputs:
      - amqp_0_9:
          urls: [ "${AMQP_URL}" ]
          queue: message
          consumer_tag: evo-conversations
          prefetch_count: 50
      - amqp_0_9:
          urls: [ "${AMQP_URL}" ]
          queue: sendmessage
          consumer_tag: evo-conversations
          prefetch_count: 50

pipeline:
  processors:
    - mapping: |
        root = if this.data.Info.ID.catch(null) == null { deleted() }
    - mapping: |
        root.message_id    = this.data.Info.ID
        root.instance_id   = this.instanceId
        root.instance_name = this.instanceName
        root.direction     = if this.data.Info.IsFromMe.or(false) { "outbound" } else { "inbound" }
        root.chat_jid      = this.data.Info.Chat
        root.sender_jid    = this.data.Info.Sender
        root.push_name     = this.data.Info.PushName
        root.is_group      = this.data.Info.IsGroup.or(false)
        root.msg_type      = this.data.Info.Type
        root.msg_ts        = this.data.Info.Timestamp
        root.body = this.data.Message.conversation
                      .or(this.data.Message.extendedTextMessage.text)
                      .or(this.data.Message.imageMessage.caption)
                      .or(this.data.Message.videoMessage.caption)
                      .or(this.data.Message.documentMessage.caption)
                      .catch(null)
        root.media_url  = this.data.Message.mediaUrl.catch(null)
        root.media_path = this.data.Message.mediaUrl
                            .re_replace_all("^https?://[^/]+/evolution-go/", "")
                            .re_replace_all("\\?.*$", "")
                            .catch(null)
        root.mimetype   = this.data.Message.mimetype.catch(null)
        let has_url = this.data.Message.mediaUrl.catch("") != ""
        root.data = if $has_url {
          this.data.merge({ "Message": this.data.Message.without("base64") })
        } else {
          this.data
        }
        root.raw   = this.data.format_json()
        root.event = this.event

output:
  fallback:
    - sql_insert:
        driver: postgres
        dsn: "${ANALYTICS_PG_DSN}"
        table: raw_messages
        columns:
          [ message_id, instance_id, instance_name, direction, chat_jid, sender_jid,
            push_name, is_group, msg_type, msg_ts, body, media_url, media_path,
            mimetype, event, raw ]
        args_mapping: |
          root = [
            this.message_id, this.instance_id, this.instance_name, this.direction,
            this.chat_jid, this.sender_jid, this.push_name, this.is_group,
            this.msg_type, this.msg_ts, this.body, this.media_url, this.media_path,
            this.mimetype, this.event, this.raw
          ]
        suffix: "ON CONFLICT (instance_id, message_id) DO NOTHING"
        batching: { count: 50, period: 2s }
    - amqp_0_9:
        urls: [ "${AMQP_URL}" ]
        exchange: ""
        key: raw_messages_dlq
        queue_declare: { enabled: true, durable: true }
```

- [ ] **Step 2 [AGENTE]: Criar `Dockerfile`**

```dockerfile
FROM docker.redpanda.com/redpandadata/connect:latest
COPY benthos.yaml /connect.yaml
CMD ["run", "/connect.yaml"]
```

- [ ] **Step 3 [AGENTE]: Criar `docker-compose.yaml`**

```yaml
services:
  evo-conversations:
    build:
      context: .
      dockerfile: Dockerfile
    restart: unless-stopped
    environment:
      AMQP_URL: "${AMQP_URL}"
      ANALYTICS_PG_DSN: "${ANALYTICS_PG_DSN}"
```

- [ ] **Step 4 [AGENTE]: Criar `.gitignore` e `README.md`**

`.gitignore`:
```
.env
```
`README.md` (resumo): o que é, envs necessárias (`AMQP_URL`, `ANALYTICS_PG_DSN`), como validar a config (`docker run ... redpandadata/connect lint /connect.yaml`), link pro spec.

- [ ] **Step 5 [AGENTE]: Lint da config Benthos (falha cedo se inválida)**

```bash
cd ~/code/evo-conversations
docker run --rm -v "$PWD/benthos.yaml:/c.yaml" docker.redpanda.com/redpandadata/connect:latest lint /c.yaml
```
Expected: sem erros de lint. Corrigir qualquer campo/função inválidos antes de prosseguir.

- [ ] **Step 6 [AGENTE]: git init + commit**

```bash
cd ~/code/evo-conversations
git init -q && git add -A
git commit -q -m "feat: benthos ingestion pipeline (AMQP -> Postgres) + R2 media refs

Claude-Session: https://claude.ai/code/session_01CgafPNy4DkTzEXFCtUQZZK"
```

- [ ] **Step 7 [VOCÊ]: Criar o repo PRIVADO no GitHub e dar push**

```bash
cd ~/code/evo-conversations
gh repo create medeiroz/evo-conversations --private --source=. --remote=origin --push
```
(ou criar pela UI do GitHub e `git remote add origin ... && git push -u origin main`.) O repo é **privado**; o deploy no Coolify será via **GitHub App** (Task 4), não por Actions.

---

## Task 4: Deploy do Benthos no Coolify + validação end-to-end

**Files:** nenhum (recurso Coolify + envs).

**Interfaces:**
- Consumes: repo `evo-conversations` (Task 3), database `evogo_analytics` (Task 2), payloads R2 (Task 1).
- Produces: linhas em `raw_messages`.

- [ ] **Step 1 [VOCÊ]: Criar o recurso no Coolify via GitHub App** — projeto **Evolution Go**, *New Resource → Private Repository (with GitHub App)* → repo `medeiroz/evo-conversations`, branch `main`, build pack **Docker Compose**. Isso também liga o **trigger de deploy automático no push**. Anotar o **UUID** do app criado (`coolify app list`).

- [ ] **Step 2 [VOCÊ]: Ligar "Connect To Predefined Network"** no app novo (Configuration → Advanced) — pra resolver `rabbitmq-...` e `rmuon5...` pela rede `coolify`.

- [ ] **Step 3 [AGENTE]: Setar as envs de segredo no app novo**

```bash
NEW=<uuid-do-evo-conversations>
coolify app env create $NEW --key AMQP_URL --value 'amqp://YIGIxNF1JlwSnfJC:koYA1W5PyGiwsn0qen5voaDNpxOYX5wh@rabbitmq-m9jkqoqkkc8q8ijslc3npmf2:5672/'
coolify app env create $NEW --key ANALYTICS_PG_DSN --value 'postgres://postgres:<MANAGED_PG_PASSWORD>@rmuon5lug96hpx0uerincy3h:5432/evogo_analytics?sslmode=disable'
```
(`<MANAGED_PG_PASSWORD>` = valor da env homônima do app evolution-go.)

- [ ] **Step 4 [AGENTE]: Deploy + aguardar `finished`**

```bash
coolify deploy uuid <uuid-do-evo-conversations>
# aguardar status "finished" em: coolify app deployments list <uuid> --format json | jq '.[0].status'
```

- [ ] **Step 5 [AGENTE]: Validar conexão do Benthos (logs)**

```bash
coolify app logs <uuid-do-evo-conversations> -n 120 | grep -iE "error|amqp|postgres|input|output|connect"
```
Expected: sem erros de conexão AMQP/Postgres; consumo ativo das filas.

- [ ] **Step 6 [AGENTE]: Validar ingestão — as mensagens enfileiradas viram linhas**

```bash
psql "postgresql://postgres:$PGPW@157.151.17.255:65344/evogo_analytics?sslmode=disable" -c \
  "SELECT direction, msg_type, is_group, count(*) FROM raw_messages GROUP BY 1,2,3 ORDER BY 4 DESC;"
```
Expected: linhas > 0; `direction` inbound/outbound; grupos com `is_group=t`.

- [ ] **Step 7 [AGENTE]: Validar mídia (media_path = chave, não URL)**

```bash
psql "postgresql://postgres:$PGPW@157.151.17.255:65344/evogo_analytics?sslmode=disable" -c \
  "SELECT msg_type, media_path, mimetype FROM raw_messages WHERE media_path IS NOT NULL LIMIT 5;"
```
Expected: `media_path` no formato `evolution-go-medias/<id>.<ext>` (sem `https://`). Se vier URL completa, ajustar o regex do `media_path` no `benthos.yaml` e redeployar.

- [ ] **Step 8 [AGENTE]: Validar dedup e filas drenando**

```bash
# filas devem estar drenando (ready -> 0) e sem crescer a DLQ
U=YIGIxNF1JlwSnfJC; P=koYA1W5PyGiwsn0qen5voaDNpxOYX5wh; B=https://rabbitmq-evolution.medeiroz.com
for q in message sendmessage raw_messages_dlq; do
  n=$(curl -s -u "$U:$P" "$B/api/queues/%2F/$q" | python3 -c "import sys,json;print(json.load(sys.stdin).get('messages'))" 2>/dev/null)
  echo "$q: $n"
done
# reprocesso não duplica: contar antes/depois de reenviar 1 msg -> igual (ON CONFLICT)
```
Expected: `message`/`sendmessage` drenando pra ~0; `raw_messages_dlq` = 0 (nada falhando); count estável em reprocesso.

- [ ] **Step 9 [AGENTE]: Registrar aprendizados** — atualizar `APRENDIZADOS-COOLIFY.md` (evolution-go) e a memória de infra com o pipeline funcional, UUID do app novo, e a DLQ.

---

## Critérios de aceite (resumo)

1. evolution-go redeploya, instâncias **reconectam sozinhas**, mídia nova vai pro R2 (`mediaUrl`, sem base64).
2. `evogo_analytics.raw_messages` existe (PK composta) e recebe linhas do Benthos.
3. `direction`/`body`/`is_group`/`media_path` corretos numa amostra.
4. Filas drenam; DLQ vazia; reprocesso não duplica.
5. Aprendizados e memória atualizados.
