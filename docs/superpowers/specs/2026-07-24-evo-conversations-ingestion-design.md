# Spec — Ingestão de conversas do WhatsApp (Fase A)

- **Data:** 2026-07-24
- **Status:** aprovado para implementação (Fase A)
- **Autor:** Flavio + Claude
- **Projeto novo:** `~/code/evo-conversations/`
- **Fase B (fora deste spec):** agente de IA de satisfação do cliente

## 1. Objetivo

Persistir **todas as mensagens de todas as instâncias** do `evolution-go` numa base
consultável, para depois (Fase B) um agente de IA analisar as conversas das empresas e
avaliar satisfação do cliente. Este spec cobre **só a ingestão** (captura → storage). O
agente de IA é um spec separado, que apenas lê o que aqui for gravado.

### Por que não usar algo pronto

- O `evolution-go` (fork Go, "local-mode, no license server") **não persiste conteúdo** de
  mensagem: a tabela `messages` só guarda metadados de status/entrega. (A Evolution API Node
  persiste, mas trocar de gateway só por isso não compensa.)
- Chatwoot guardaria conversas, mas é um helpdesk pesado com schema de atendimento — encaixe
  errado para uma base analítica.
- Conclusão: capturar o stream AMQP no próprio Postgres é o caminho mínimo e sob medida.

## 2. Escopo

**Dentro:**
- Habilitar mídia → Cloudflare R2 no `evolution-go` (`MINIO_ENABLED`).
- Consumer config-only (Redpanda Connect / Benthos) lendo as filas `message` e `sendmessage`.
- Database nova `evogo_analytics` no Postgres gerenciado, tabela `raw_messages`.
- Dedup idempotente, captura de grupos, extração de texto, referência de mídia por caminho.

**Fora (Fase B / follow-ups):**
- Agente de IA, prompt, scoring de satisfação.
- Transcrição de áudio; regeneração de URL assinada de mídia.
- Retenção / LGPD / criptografia at-rest.
- Particionamento por mês da tabela.
- `CONNECT_ON_STARTUP=true` no evolution-go (follow-up operacional — ver §10).
- Remover o publish `0.0.0.0:5672` do RabbitMQ (hardening — ver §10).

## 3. Arquitetura

```
evolution-go (N instâncias)                          [repo atual]
  ├─ mídia  ──────────────► Cloudflare R2 (bucket `evolution-go`,
  │                          prefixo de objeto `evolution-go-medias/`)
  └─ evento AMQP (payload já com `mediaUrl`, sem base64)
        │  filas `message` (recebidas + enviadas do celular, IsFromMe=true)
        │        `sendmessage` (enviadas via API)
        ▼
  Benthos (Redpanda Connect)                         [repo novo evo-conversations]
    input amqp_0_9 (2 filas) → mapping Bloblang → sql_insert (ON CONFLICT)
    1 container, na rede Docker predefinida `coolify`
        ▼
  Postgres gerenciado (rmuon5lug96hpx0uerincy3h:5432)
    database NOVA `evogo_analytics` → tabela `raw_messages`
        ▼
  [Fase B] agente de IA lê `raw_messages` em batch (offline)
```

O `evolution-go` publica o mesmo payload por webhook e AMQP. Ligar o R2 remove o base64 de
**todo** o pipeline (encolhe muito os payloads), não só do nosso consumer.

## 4. Componente 1 — evolution-go: mídia no R2

O código já tem storage S3/MinIO nativo. Lógica em `pkg/whatsmeow/service/whatsmeow.go`
(≈L1558): se `MinioEnabled`, baixa a mídia, faz `PutObject` e injeta no payload
`Message.mediaUrl` (URL **assinada, validade 7 dias**) + `Message.mimetype`; **senão**, injeta
`Message.base64`. É um `if/else` — com R2 ligado, base64 nunca é gerado.

- Objeto gravado: chave `evolution-go-medias/<messageID><ext>` no bucket `evolution-go`.
- `Store()` (`pkg/storage/minio/media_storage.go`) devolve **presigned URL de 7 dias**; o
  objeto em si é permanente. Por isso guardamos a **chave estável** (`media_path`) e não só a
  URL assinada — a URL é regenerada quando necessário (Fase B).
- `setBucketPolicy` deve **dar warning no R2** (R2 não suporta esse policy público) — o código
  loga e continua usando presigned URLs. Comportamento esperado, não é erro.

### Config (envs do Coolify no app `evolution-go`, uuid `v6avb69d6saxp5whzm5bgyyt`)

Envs mágicas do MinIO no evolution-go: `MINIO_ENABLED`, `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`,
`MINIO_SECRET_KEY`, `MINIO_BUCKET`, `MINIO_USE_SSL`, `MINIO_REGION`.

| Env | Valor |
|-----|-------|
| `MINIO_ENABLED` | `true` |
| `MINIO_ENDPOINT` | `<ACCOUNT_ID>.r2.cloudflarestorage.com` (host, sem `https://`) |
| `MINIO_ACCESS_KEY` | Access Key ID do token S3 do R2 (segredo Coolify) |
| `MINIO_SECRET_KEY` | Secret Access Key do token S3 do R2 (segredo Coolify) |
| `MINIO_BUCKET` | `evolution-go` |
| `MINIO_USE_SSL` | `true` |
| `MINIO_REGION` | `auto` (padrão do R2) |

No `docker-compose.yaml` do evolution-go: trocar `MINIO_ENABLED: "false"` → `"true"` e
adicionar as demais linhas referenciando `${...}` (segredos ficam nas envs do Coolify, fora do
git). Requer **criar um token de API do R2 (S3)** com acesso ao bucket `evolution-go`.

> Nota: as mídias já enfileiradas hoje (as ~20 mensagens de teste) continuam como `base64`;
> só as **novas** virão como `mediaUrl`. O consumer trata os dois (ver §7).

## 5. Componente 2 — repo `evo-conversations`

```
~/code/evo-conversations/
├── benthos.yaml            # pipeline (input AMQP → mapping → sql_insert)
├── migrations/
│   └── 001_init.sql        # cria a database + tabela raw_messages
├── docker-compose.yaml     # 1 container benthos, na rede `coolify`
└── README.md               # setup, como rodar a migration, envs
```

Deploy no Coolify como app Docker Compose, com **"Connect To Predefined Network"** ligado (pra
resolver `rabbitmq-m9jkqoqkkc8q8ijslc3npmf2` e `rmuon5lug96hpx0uerincy3h` pela rede `coolify`).
Segredos (`AMQP_URL`, `ANALYTICS_PG_DSN`) via env do Coolify.

## 6. Componente 3 — schema `raw_messages`

`migrations/001_init.sql`:

```sql
-- rodar 1x conectado ao Postgres gerenciado (rmuon5lug96hpx0uerincy3h):
CREATE DATABASE evogo_analytics;

-- conectar em evogo_analytics e criar:
CREATE TABLE raw_messages (
  message_id    text PRIMARY KEY,          -- data.Info.ID (dedup natural)
  instance_id   text NOT NULL,             -- envelope.instanceId
  instance_name text,                      -- envelope.instanceName
  direction     text NOT NULL,             -- 'inbound' | 'outbound' (de Info.IsFromMe)
  chat_jid      text,                      -- Info.Chat (@g.us grupo; @s.whatsapp.net/@lid 1:1)
  sender_jid    text,                      -- Info.Sender
  push_name     text,                      -- Info.PushName
  is_group      boolean,                   -- Info.IsGroup
  msg_type      text,                      -- Info.Type (text|media|reaction|...)
  msg_ts        timestamptz,               -- Info.Timestamp (RFC3339 c/ tz)
  body          text,                      -- texto extraído (null p/ mídia sem legenda)
  media_url     text,                      -- Message.mediaUrl (presigned 7d; pode expirar)
  media_path    text,                      -- chave estável no R2 (mediaUrl sem query string)
  mimetype      text,                      -- Message.mimetype
  event         text,                      -- envelope.event ('Message' | 'SendMessage')
  raw           jsonb NOT NULL,            -- payload data completo (sem base64)
  ingested_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX raw_messages_instance_ts_idx ON raw_messages (instance_id, msg_ts);
CREATE INDEX raw_messages_chat_ts_idx     ON raw_messages (chat_jid, msg_ts);
```

## 7. Componente 4 — pipeline Benthos

Paths **confirmados contra payloads reais** puxados das filas. Envelope:
`{ data, event, instanceId, instanceName, instanceToken }`. `data.Info` (PascalCase):
`ID, Chat, Sender, PushName, IsFromMe, IsGroup, Type, Timestamp`. Texto em
`data.Message.conversation` ou `data.Message.extendedTextMessage.text`; mídia em
`data.Message.mediaUrl`/`mimetype` (com R2 ligado).

`benthos.yaml` (referência — Bloblang final validado na implementação com 1 mídia real):

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

        # texto: conversation -> extendedTextMessage.text -> captions
        root.body = this.data.Message.conversation
                      .or(this.data.Message.extendedTextMessage.text)
                      .or(this.data.Message.imageMessage.caption)
                      .or(this.data.Message.videoMessage.caption)
                      .or(this.data.Message.documentMessage.caption)
                      .catch(null)

        # mídia: já vem como URL assinada quando MINIO/R2 ligado
        root.media_url  = this.data.Message.mediaUrl.catch(null)
        root.media_path = this.data.Message.mediaUrl.re_replace_all("\\?.*$", "").catch(null)
        root.mimetype   = this.data.Message.mimetype.catch(null)

        # payload cru; defesa: remove base64 caso alguma msg antiga o traga
        root.raw   = this.data.without("Message").merge({ "Message": this.data.Message.without("base64").catch({}) })
        root.event = this.event

output:
  sql_insert:
    driver: postgres
    dsn: "${ANALYTICS_PG_DSN}"
    table: raw_messages
    columns:
      - message_id
      - instance_id
      - instance_name
      - direction
      - chat_jid
      - sender_jid
      - push_name
      - is_group
      - msg_type
      - msg_ts
      - body
      - media_url
      - media_path
      - mimetype
      - raw
      - event
    args_mapping: |
      root = [
        this.message_id, this.instance_id, this.instance_name, this.direction,
        this.chat_jid, this.sender_jid, this.push_name, this.is_group,
        this.msg_type, this.msg_ts, this.body, this.media_url, this.media_path,
        this.mimetype, this.raw, this.event
      ]
    suffix: "ON CONFLICT (message_id) DO NOTHING"
    batching:
      count: 50
      period: 2s
```

A coluna `event` (§6) guarda `envelope.event` (`Message` vs `SendMessage`) — barato e ajuda no
debug de origem da mensagem.

## 8. Fluxo de dados e dedup

1. Mensagem no WhatsApp → evolution-go processa, sobe mídia no R2, publica no AMQP.
2. Benthos consome, mapeia, faz `INSERT ... ON CONFLICT (message_id) DO NOTHING`.
3. As mensagens já enfileiradas entram assim que o Benthos subir (nada se perde).

**Dedup:** RabbitMQ é *at-least-once*; a PK `message_id` + `ON CONFLICT DO NOTHING` tornam
reentrega inofensiva. Uma mesma mensagem que apareça em `message` e `sendmessage` grava uma
linha só.

## 9. Resiliência e erros

- Filas **quorum + durable**, `DeliveryMode: Persistent` (produtor evolution-go) → sobrevivem a
  restart do broker.
- Benthos dá **ack só após o insert** → crash do consumer devolve a mensagem à fila.
- **Postgres fora do ar:** Benthos re-tenta e segura na fila (backpressure); não perde.
- **Falha de upload no R2 (evolution-go):** o código loga e **continua sem `mediaUrl`** (nem
  base64) — a mensagem ainda é gravada, só sem referência de mídia. Aceitável na Fase A.
- **URL assinada expira em 7 dias:** por isso `media_path` (chave estável) é a fonte de verdade
  para acessar o objeto depois.

## 10. Segurança / segredos

- `AMQP_URL`, `ANALYTICS_PG_DSN`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`: **envs do Coolify**,
  nunca no git. No compose só `${...}`.
- Token de API do R2 **escopado ao bucket `evolution-go`** (não reusar credenciais amplas).
- Follow-ups (fora deste spec): `CONNECT_ON_STARTUP=true` no evolution-go (senão todo redeploy
  derruba a captura até reconectar as instâncias na mão); remover o publish `0.0.0.0:5672` do
  service RabbitMQ (comunicação é interna pela rede `coolify`).

## 11. Validação (critérios de aceite)

1. Migration aplicada → `evogo_analytics.raw_messages` existe com os índices.
2. Após ligar R2 no evolution-go e enviar 1 mídia: o payload novo traz `mediaUrl` (sem
   `base64`), e o objeto aparece no bucket `evolution-go` sob `evolution-go-medias/`.
3. Subir o Benthos → as mensagens enfileiradas viram linhas em `raw_messages`.
4. Conferir uma amostra: `direction` correto (IsFromMe), `body` preenchido p/ texto,
   `is_group=true` nas de grupo, `media_path` preenchido p/ mídia.
5. Reenviar a mesma mensagem (ou reprocessar) → `count(*)` não muda (dedup ok).

## 12. Referência (infra)

| Recurso | Valor |
|---------|-------|
| App `evolution-go` | `v6avb69d6saxp5whzm5bgyyt` |
| Postgres gerenciado | `rmuon5lug96hpx0uerincy3h` (host interno `:5432`) |
| Database nova | `evogo_analytics` |
| RabbitMQ (service) | `m9jkqoqkkc8q8ijslc3npmf2`, host interno `rabbitmq-m9jkqoqkkc8q8ijslc3npmf2:5672` |
| Filas | `message`, `sendmessage` (quorum, durable) |
| Bucket R2 (mídia) | `evolution-go` (prefixo `evolution-go-medias/`) |
| Rede Docker compartilhada | `coolify` (predefined network) |

## 13. Ordem de implementação (visão macro)

1. Criar token S3 do R2 + ligar `MINIO_*` no evolution-go → redeploy → validar mídia no bucket.
2. Rodar `001_init.sql` (criar `evogo_analytics` + tabela).
3. Criar repo `evo-conversations` (benthos.yaml, compose, README).
4. Deploy do Benthos no Coolify (rede `coolify`, envs) → validar ingestão + dedup.

O plano detalhado (passo a passo executável) sai na etapa de writing-plans.
