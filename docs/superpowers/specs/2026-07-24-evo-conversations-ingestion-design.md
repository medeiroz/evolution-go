# Spec — Ingestão de conversas do WhatsApp (Fase A)

- **Data:** 2026-07-24
- **Status:** aprovado para implementação (Fase A) — revisado (Opus review incorporado)
- **Autor:** Flavio + Claude
- **Projeto novo:** `~/code/evo-conversations/`
- **Fase B (fora deste spec):** agente de IA de satisfação do cliente

> **Revisão adversarial (2026-07-24):** um subagente Opus verificou este spec contra o
> código. Correções incorporadas: PK composta `(instance_id, message_id)` (o `Info.ID` do
> WhatsApp **não** é único global); `raw` serializado com `format_json()` antes do insert;
> `CONNECT_ON_STARTUP=true` movido para dentro do escopo; guard de poison-message + DLQ;
> `media_path` = chave do objeto (sem host/bucket); avisos de `panicIfEmpty` e de assimetria
> `message`/`sendmessage`.

## 1. Objetivo

Persistir **todas as mensagens de todas as instâncias** do `evolution-go` numa base
consultável, para depois (Fase B) um agente de IA analisar as conversas das empresas e
avaliar satisfação do cliente. Este spec cobre **só a ingestão** (captura → storage).

### Por que não usar algo pronto
- O `evolution-go` (fork Go, "local-mode, no license server") **não persiste conteúdo** de
  mensagem: a tabela `messages` só guarda metadados de status/entrega.
- Chatwoot guardaria conversas, mas é um helpdesk pesado com schema de atendimento — encaixe
  errado para base analítica.
- Conclusão: capturar o stream AMQP no próprio Postgres é o caminho mínimo e sob medida.

## 2. Escopo

**Dentro:**
- Habilitar mídia → Cloudflare R2 no `evolution-go` (`MINIO_ENABLED`), **junto** com
  `CONNECT_ON_STARTUP=true` (ver §4 — obrigatório no mesmo redeploy).
- Consumer config-only (Redpanda Connect / Benthos) lendo as filas `message` e `sendmessage`.
- Database nova `evogo_analytics` no Postgres gerenciado, tabela `raw_messages`.
- Dedup idempotente por `(instance_id, message_id)`, captura de grupos, extração de texto,
  referência de mídia por chave de objeto, guard de poison-message + DLQ.

**Fora (Fase B / follow-ups):**
- Agente de IA, prompt, scoring de satisfação.
- Transcrição de áudio; regeneração da URL assinada de mídia (a partir de `media_path`).
- Rotear o **send path via API** pelo R2 (hoje ele base64-encoda — ver §4/§9); só relevante se
  vocês passarem a enviar mídia via API. Hoje **não usam** (envio é pelo celular).
- Retenção / LGPD / criptografia at-rest; particionamento por mês.
- Remover o publish `0.0.0.0:5672` do RabbitMQ (hardening — ver §10).
- Normalizar o vocabulário de `msg_type` entre as duas filas (ver §7).

## 3. Arquitetura

```
evolution-go (N instâncias)                          [repo atual]
  ├─ mídia RECEBIDA / enviada do celular ─► Cloudflare R2 (bucket `evolution-go`,
  │                                         objeto `evolution-go-medias/<id><ext>`)
  └─ evento AMQP
        │  fila `message`      : recebidas + enviadas do celular (IsFromMe=true) → payload com `mediaUrl`
        │  fila `sendmessage`  : só envios via API (NÃO usados hoje → praticamente vazia)
        ▼
  Benthos (Redpanda Connect)                         [repo novo evo-conversations]
    input amqp_0_9 (2 filas) → mapping Bloblang → sql_insert (ON CONFLICT) + DLQ
    1 container, na rede Docker predefinida `coolify`
        ▼
  Postgres gerenciado (rmuon5lug96hpx0uerincy3h:5432)
    database NOVA `evogo_analytics` → tabela `raw_messages`
        ▼
  [Fase B] agente de IA lê `raw_messages` em batch (offline)
```

**Importante (assimetria das filas):** ligar o R2 remove o base64 do caminho de **recebimento**
(fila `message`, `whatsmeow.go:1559-1587`, `if MinioEnabled { mediaUrl } else { base64 }`). O
caminho de **envio via API** (`send_service.go:2981`) base64-encoda **incondicionalmente** e não
usa MinIO — então a fila `sendmessage` ainda traria base64. Como envio via API **não é usado**,
isso não ocorre na prática; ainda assim o mapping faz **strip condicional** (§7) pra não perder
referência de mídia caso apareça.

## 4. Componente 1 — evolution-go: mídia no R2 (+ CONNECT_ON_STARTUP)

Código já tem storage S3/MinIO nativo. Em `pkg/whatsmeow/service/whatsmeow.go` (≈L1558): se
`MinioEnabled`, baixa a mídia, `PutObject` e injeta `Message.mediaUrl` (URL **assinada, 7 dias**)
+ `Message.mimetype`; senão, `Message.base64`. Com R2 ligado, base64 não é gerado **no recebimento**.

- Objeto: chave `evolution-go-medias/<messageID><ext>` no bucket `evolution-go`
  (`media_storage.go:40-41`, `Store` :96-108).
- `Store()` devolve **presigned URL de 7 dias**; o objeto é permanente. Guardamos a **chave do
  objeto** (`media_path`), não a URL assinada, e regeneramos a URL quando preciso (Fase B).
- `setBucketPolicy` **dá warning no R2** e o código **continua** usando presigned URLs
  (`media_storage.go:75-80`) — comportamento esperado, não é erro.

### ⚠️ CONNECT_ON_STARTUP — obrigatório no mesmo redeploy
Default é `false` (`config.go:243-245`). Ligar o MinIO exige **redeploy**, que derruba as
instâncias; com `false` elas **não reconectam** e a captura **para em silêncio** até reconexão
manual (aconteceu nesta sessão). Portanto: setar **`CONNECT_ON_STARTUP=true`** na **mesma**
alteração que liga `MINIO_ENABLED`. Sem isso, o próprio ato de shippar a Fase A causa a perda
que se quer evitar.

### ⚠️ Config parcial derruba o app (crash, não falha suave)
`loadMinioConfig` faz `panicIfEmpty` em `MINIO_ENDPOINT`, `MINIO_ACCESS_KEY`,
`MINIO_SECRET_KEY`, `MINIO_BUCKET` (`config.go:399-410`). Se ligar `MINIO_ENABLED=true` com
**qualquer** desses vazio, o evolution-go **panica no boot** (todas as instâncias caem). Rollout
tem que ser atômico: todas as envs juntas.

### Config (envs do Coolify no app `evolution-go`, uuid `v6avb69d6saxp5whzm5bgyyt`)

| Env | Valor |
|-----|-------|
| `MINIO_ENABLED` | `true` |
| `MINIO_ENDPOINT` | `<ACCOUNT_ID>.r2.cloudflarestorage.com` (host, sem `https://`) |
| `MINIO_ACCESS_KEY` | Access Key ID do token S3 do R2 (segredo Coolify) |
| `MINIO_SECRET_KEY` | Secret Access Key do token S3 do R2 (segredo Coolify) |
| `MINIO_BUCKET` | `evolution-go` |
| `MINIO_USE_SSL` | `true` |
| `MINIO_REGION` | `auto` |
| `CONNECT_ON_STARTUP` | `true` |

No `docker-compose.yaml`: `MINIO_ENABLED: "true"`, `CONNECT_ON_STARTUP: "true"`, e as demais
`MINIO_*` referenciando `${...}` (segredos nas envs do Coolify, fora do git). Requer criar um
**token de API do R2 (S3) escopado ao bucket `evolution-go`**.

> Nota: as ~20 mídias já enfileiradas hoje continuam como `base64`; com o strip condicional (§7)
> elas entram **sem** referência de mídia (aceitável — ver §11).

## 5. Componente 2 — repo `evo-conversations`

```
~/code/evo-conversations/
├── benthos.yaml            # pipeline (input AMQP → mapping → sql_insert + DLQ)
├── migrations/
│   └── 001_init.sql        # cria a database + tabela raw_messages
├── docker-compose.yaml     # 1 container benthos, na rede `coolify`
└── README.md               # setup, migration, envs
```

Deploy no Coolify como app Docker Compose, com **"Connect To Predefined Network"** ligado (pra
resolver `rabbitmq-m9jkqoqkkc8q8ijslc3npmf2` e `rmuon5lug96hpx0uerincy3h` pela rede `coolify`).
Segredos (`AMQP_URL`, `ANALYTICS_PG_DSN`) via env do Coolify. `ANALYTICS_PG_DSN` deve incluir
`sslmode=disable` (mesma rede interna).

## 6. Componente 3 — schema `raw_messages`

`migrations/001_init.sql`:

```sql
-- rodar 1x conectado ao Postgres gerenciado (rmuon5lug96hpx0uerincy3h):
CREATE DATABASE evogo_analytics;

-- conectar em evogo_analytics e criar:
CREATE TABLE raw_messages (
  message_id    text NOT NULL,             -- data.Info.ID (MESMO p/ todos os destinatários!)
  instance_id   text NOT NULL,             -- envelope.instanceId
  instance_name text,                      -- envelope.instanceName
  direction     text NOT NULL,             -- 'inbound' | 'outbound' (de Info.IsFromMe)
  chat_jid      text,                      -- Info.Chat (@g.us grupo; @s.whatsapp.net/@lid 1:1)
  sender_jid    text,                      -- Info.Sender
  push_name     text,                      -- Info.PushName
  is_group      boolean,                   -- Info.IsGroup
  msg_type      text,                      -- Info.Type (vocab difere por fila — ver §7)
  msg_ts        timestamptz,               -- Info.Timestamp (RFC3339 c/ tz → cast seguro)
  body          text,                      -- texto extraído (null p/ mídia sem legenda)
  media_url     text,                      -- Message.mediaUrl (presigned 7d; pode expirar)
  media_path    text,                      -- CHAVE do objeto no R2: evolution-go-medias/<id><ext>
  mimetype      text,                      -- Message.mimetype
  event         text,                      -- envelope.event ('Message' | 'SendMessage')
  raw           jsonb NOT NULL,            -- payload data completo (base64 removido só se houver mediaUrl)
  ingested_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (instance_id, message_id)    -- Info.ID NÃO é único global; dedup por instância
);

CREATE INDEX raw_messages_instance_ts_idx ON raw_messages (instance_id, msg_ts);
CREATE INDEX raw_messages_chat_ts_idx     ON raw_messages (chat_jid, msg_ts);
```

**Por que PK composta:** o `Info.ID` do WhatsApp é atribuído pelo remetente e **todos os
destinatários veem o mesmo ID**. Duas instâncias suas no mesmo grupo recebem o mesmo `Info.ID`;
uma PK só em `message_id` descartaria a segunda linha (perda silenciosa) e embaralharia a
`direction`. `(instance_id, message_id)` mantém uma linha **por instância**.

## 7. Componente 4 — pipeline Benthos

Paths confirmados contra payloads reais. Envelope:
`{ data, event, instanceId, instanceName, instanceToken }`. `data.Info` (PascalCase):
`ID, Chat, Sender, PushName, IsFromMe, IsGroup, Type, Timestamp`. Texto em
`data.Message.conversation` / `data.Message.extendedTextMessage.text`; mídia (recebida) em
`data.Message.mediaUrl`/`mimetype`.

`benthos.yaml` (referência — Bloblang final validado na implementação contra 1 mídia real do R2,
sobretudo o parsing de `media_path`, que depende do estilo de URL — path-style vs virtual-host —
que o cliente MinIO gera pro R2):

```yaml
input:
  broker:
    inputs:
      - amqp_0_9:
          urls: [ "${AMQP_URL}" ]
          queue: message
          consumer_tag: evo-conversations       # reuso do tag nos 2 inputs é ok (canais separados)
          prefetch_count: 50
      - amqp_0_9:
          urls: [ "${AMQP_URL}" ]
          queue: sendmessage
          consumer_tag: evo-conversations
          prefetch_count: 50

pipeline:
  processors:
    - mapping: |
        # --- guard poison-message: sem ID não dá pra deduplicar/gravar -> descarta ---
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

        # texto: conversation -> extendedTextMessage.text -> captions
        root.body = this.data.Message.conversation
                      .or(this.data.Message.extendedTextMessage.text)
                      .or(this.data.Message.imageMessage.caption)
                      .or(this.data.Message.videoMessage.caption)
                      .or(this.data.Message.documentMessage.caption)
                      .catch(null)

        # mídia recebida: já vem como URL assinada quando R2 ligado
        root.media_url  = this.data.Message.mediaUrl.catch(null)
        # media_path = CHAVE do objeto (sem scheme/host/bucket e sem query). Regex final
        # validado contra uma URL real do R2.
        root.media_path = this.data.Message.mediaUrl
                            .re_replace_all("^https?://[^/]+/evolution-go/", "")
                            .re_replace_all("\\?.*$", "")
                            .catch(null)
        root.mimetype   = this.data.Message.mimetype.catch(null)

        # strip CONDICIONAL de base64: só remove quando já há mediaUrl (senão preservaria
        # a única referência de mídia — caso do send path via API).
        let has_url = this.data.Message.mediaUrl.catch("") != ""
        root.data = if $has_url {
          this.data.merge({ "Message": this.data.Message.without("base64") })
        } else {
          this.data
        }
        # raw serializado como texto JSON (lib/pq rejeita map/objeto como parâmetro)
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
    # DLQ: o que falhar no insert cai numa fila morta em vez de travar o pipeline
    - amqp_0_9:
        urls: [ "${AMQP_URL}" ]
        exchange: ""
        key: raw_messages_dlq          # fila morta (declarar durable)
```

> `msg_type` tem **vocabulários diferentes** por fila: recebimento usa `Info.Type`
> (`text`/`media`/`reaction`…), envio via API usaria `messageType`
> (`ExtendedTextMessage`/`ImageMessage`…, `send_service.go:2945`). Normalização fica como
> follow-up; como envio via API não é usado, na prática só aparece o vocab de recebimento.

## 8. Fluxo de dados e dedup

1. Mensagem no WhatsApp → evolution-go processa, sobe mídia no R2, publica no AMQP.
2. Benthos: guard (descarta sem `ID`) → mapping → `INSERT ... ON CONFLICT (instance_id,
   message_id) DO NOTHING`.
3. Mensagens já enfileiradas entram quando o Benthos subir.

**Dedup:** RabbitMQ é *at-least-once*; a PK composta + `ON CONFLICT DO NOTHING` tornam reentrega
da **mesma instância** inofensiva, **sem** colapsar linhas de instâncias diferentes.

**A esclarecer na implementação (echo semantics):** se um envio via API também gera um `Message`
self-echo (mesmo `Info.ID`, mesma instância) na fila `message`, as duas entradas colidem na PK e
uma vence. Como envio via API não é usado, é irrelevante hoje; documentado por precaução.

## 9. Resiliência e erros

- Filas `message`/`sendmessage` **quorum + durable**, `DeliveryMode: Persistent`
  (`rabbitmq_producer.go:198-201,142`) → sobrevivem a restart do broker.
- Benthos **ack só após o output** → crash devolve a mensagem à fila.
- **Poison-message** (evento sem `Info.ID`, ex. protocolos): o guard (§7) **descarta** antes do
  insert, evitando requeue infinito que travaria as duas filas.
- **Falha de insert** (ex. constraint inesperada): cai na **DLQ** `raw_messages_dlq` via
  `output.fallback`, sem travar o pipeline. Inspecionar a DLQ periodicamente.
- **Postgres fora do ar:** Benthos re-tenta e segura na fila (backpressure). Pool/backoff usam os
  defaults do `sql_insert`; revisar se necessário.
- **Falha de upload no R2 (evolution-go):** loga e **continua sem `mediaUrl`** (nem base64) — a
  mensagem é gravada sem referência de mídia. Aceitável na Fase A.
- **Presigned expira em 7 dias:** por isso `media_path` (chave do objeto) é a fonte de verdade.

## 10. Segurança / segredos

- `AMQP_URL`, `ANALYTICS_PG_DSN`, `MINIO_ACCESS_KEY`, `MINIO_SECRET_KEY`: **envs do Coolify**,
  nunca no git. No compose só `${...}`.
- Token S3 do R2 **escopado ao bucket `evolution-go`** (não reusar credenciais amplas).
- Follow-up de hardening (fora deste spec): remover o publish `0.0.0.0:5672` do service RabbitMQ
  (comunicação é interna pela rede `coolify`).

## 11. Validação (critérios de aceite)

1. Migration aplicada → `evogo_analytics.raw_messages` existe com PK `(instance_id, message_id)`
   e os índices.
2. Após ligar R2 + `CONNECT_ON_STARTUP=true` e redeploy: instâncias **reconectam sozinhas**
   (checar `/instance/all` → `connected: true`); enviar 1 mídia → payload novo traz `mediaUrl`
   (sem `base64`) e o objeto aparece no bucket sob `evolution-go-medias/`.
3. Subir o Benthos → mensagens enfileiradas viram linhas em `raw_messages`.
4. Amostra: `direction` correto (IsFromMe), `body` p/ texto, `is_group=true` nas de grupo,
   `media_path` = **chave** do objeto (não URL completa) p/ mídia nova.
5. **Dedup:** reprocessar a mesma mensagem → `count(*)` não muda. Confirmar que duas instâncias
   no mesmo grupo geram **duas** linhas (uma por instância), não uma.
6. **Poison-message:** evento sem `Info.ID` não trava o pipeline (descartado) e não vira loop.
7. **Backfill:** as ~20 mensagens já enfileiradas (base64) entram **sem** `media_url`/`media_path`
   (sem referência de mídia) — comportamento esperado, não é falha.

## 12. Referência (infra)

| Recurso | Valor |
|---------|-------|
| App `evolution-go` | `v6avb69d6saxp5whzm5bgyyt` |
| Postgres gerenciado | `rmuon5lug96hpx0uerincy3h` (host interno `:5432`) |
| Database nova | `evogo_analytics` |
| RabbitMQ (service) | `m9jkqoqkkc8q8ijslc3npmf2`, host `rabbitmq-m9jkqoqkkc8q8ijslc3npmf2:5672` |
| Filas | `message`, `sendmessage` (quorum, durable) + `raw_messages_dlq` (a criar) |
| Bucket R2 (mídia) | `evolution-go` (objeto `evolution-go-medias/<id><ext>`) |
| Rede Docker compartilhada | `coolify` (predefined network) |

## 13. Ordem de implementação (visão macro)

1. Criar token S3 do R2 (escopo bucket `evolution-go`) + ligar `MINIO_*` **e**
   `CONNECT_ON_STARTUP=true` no evolution-go (atômico) → redeploy → validar reconexão + mídia no
   bucket.
2. Rodar `001_init.sql` (criar `evogo_analytics` + tabela + índices).
3. Criar repo `evo-conversations` (benthos.yaml, compose, README) + declarar a DLQ.
4. Deploy do Benthos no Coolify (rede `coolify`, envs) → validar ingestão, dedup e guard.

O plano detalhado (passo a passo executável) sai na etapa de writing-plans.
