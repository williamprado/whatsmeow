# Plano de integração de chamadas de voz (WaCalls → fork whatsmeow)

> **Status: PLANO PARA APROVAÇÃO — sem código ainda.**
> Etapa 2 da iniciativa de chamadas. Implementação só começa após aprovação
> explícita do Tech Lead, em branch separada e testada apenas em conta descartável.
>
> ⚠️ **Risco de banimento.** VoIP por lib não-oficial usa protocolo reverso-
> engenheirado e crypto/constantes hard-coded da WhatsApp. É **mais sensível que
> botões** ao banimento. Nunca testar/usar na conta de produção (atendzappy).

Referência estudada: fork **`williamprado/WaCalls`** (`module wacalls`, Go 1.26.4,
pin `go.mau.fi/whatsmeow v0.0.0-20260622185415-5f04eac6dbbb`). É uma **aplicação**
(servidor + cliente React), não uma lib publicável; a lógica de VoIP vive em
`internal/voip/**` e o acoplamento com whatsmeow em `internal/wa` + `cmd/server`.

---

## 1. O que precisa ser portado/integrado

O objetivo é dar ao nosso fork a capacidade de **chamada de voz 1:1** (originar,
receber, aceitar/rejeitar, encerrar) com áudio bidirecional.

Camadas do WaCalls a trazer (todas pure-Go, sem CGO):

| Origem (WaCalls) | Responsabilidade | Trazer? |
|---|---|---|
| `internal/voip/core` | Tipos de domínio + **interface `VoipSocket`** (desacopla a lógica de chamada do whatsmeow) | ✅ porta direta |
| `internal/voip/signaling` | Build/parse do stanza `<call>` (offer/accept/preaccept/terminate/reject/transport/mute), crypto da call-key, parse do relay-ack | ✅ porta direta |
| `internal/voip/call` | **`CallManager`** — orquestra uma chamada (FSM, `StartCall`/`AcceptCall`/`HandleCall*`, callbacks) | ✅ porta direta |
| `internal/voip/media` | Wrapper do codec MLow, RTP, **SRTP feito à mão** (AES-CTR + HMAC-SHA1, tag 4 bytes), derivação de SSRC/keys (HKDF) | ✅ porta direta |
| `internal/voip/media/mlow` | **Codec MLow puro-Go** (16 kHz, frames de 960 amostras), com testes de vetor | ✅ porta direta (in-tree, MIT) |
| `internal/voip/transport` | Cliente de relay via **pion/webrtc DataChannel** + STUN feito à mão + subscrições SSRC | ✅ porta direta |
| `internal/voip/wanode` | Helpers de JID/`waBinary.Node` | ✅ (ou usar os do whatsmeow) |
| `internal/wa` (`VoipSocket` adapter) | **Única peça que toca o whatsmeow** (via `DangerousInternals()`) | ✅ **adaptar ao nosso client** |
| `cmd/server`, `client/` | Servidor HTTP/SSE + UI React (PCM↔WebRTC com o browser) | ❌ fora de escopo da lib; serve de exemplo |

**Forma de integração recomendada:** criar um subpacote no nosso fork, ex.
`go.mau.fi/whatsmeow/voip` (ou repositório companion `whatsmeow-voip`), contendo
`internal/voip/**` re-empacotado como **biblioteca importável** (hoje é `internal/`
de uma app), com o adapter `VoipSocket` falando com `*whatsmeow.Client`. Manter o
acoplamento concentrado em **um arquivo** (o adapter), espelhando o desenho atual.

## 2. Dependências e restrições de build

- **pion/webrtc/v4** (`v4.2.15`) — única dependência externa pesada de runtime.
  Só são usados PeerConnection/DataChannel/SDP; **STUN/SRTP/RTP são feitos à mão**
  no próprio WaCalls (a superfície da pion realmente exercida é pequena).
- **MLow** é **in-tree, puro-Go, MIT** (sem módulo separado, sem DLL, sem CGO).
- **Sem CGO, sem ffmpeg, sem libs de sistema.** SQLite usado é `modernc.org/sqlite`
  (pure-Go) — e nem é necessário para a lib de chamada (é do app).
- **Go 1.26+** exigido (o nosso fork já está em toolchain 1.26.x — compatível).
- Impacto no `go.mod` do fork: **adiciona pion/webrtc/v4 e sua árvore transitiva**
  (~15 módulos pion indiretos). Decisão de produto: aceitar esse peso no módulo
  principal **ou** isolar a VoIP num módulo/submódulo separado para não inflar o
  `go.mod` de quem só usa mensagens. **Recomendo módulo/submódulo separado.**

## 3. Pontos de acoplamento com o whatsmeow

O acoplamento é **deliberadamente afunilado** pela interface `core.VoipSocket`,
com uma única implementação. Todo `internal/voip/**` depende só da interface +
tipos de valor do whatsmeow (`binary.Node/Attrs`, `types.JID`, `proto/waE2E`).

### 3a. Receber `<call>` — **100% API pública** (nenhuma mudança no core)
- `client.AddEventHandler(...)` e consumir os eventos **já despachados** pelo
  whatsmeow: `events.CallOffer`, `events.CallAccept`, `events.CallTransport`,
  `events.CallTerminate`, `events.CallReject`. (Confirmado: `call.go` do whatsmeow
  já faz `dispatchEvent(&events.CallOffer{...})` etc.) Cada evento traz
  `From types.JID` e `Data *waBinary.Node`.
- **Implicação:** receber chamadas **não exige patch no whatsmeow**.

### 3b. Enviar `<call>` + Signal/USync/crypto — via `DangerousInternals()` (exportado)
O adapter usa `cli.DangerousInternals()` — **tudo exportado** no whatsmeow:

| Necessidade | Chamada whatsmeow |
|---|---|
| Enviar nó | `DangerousInternalClient.SendNode(ctx, node)` |
| Query req/resp | `WaitResponse(id)` + `SendNode` + `CancelResponse` |
| IDs próprios | `GetOwnID()` / `GetOwnLID()` |
| device-identity | `MakeDeviceIdentityNode()` |
| Cifrar call-key por device | `EncryptMessageForDevices(ctx, devices, id, plaintext, dsm, encAttrs)` |
| Decifrar call-key do par | `DecryptDM(ctx, encChild, from, isPreKey, ts)` |
| Devices (USync) | `cli.GetUserDevices(ctx, jids)` (público) |
| LID de PN | `cli.Store.LIDs.GetLIDForPN`, `cli.GetUserInfo` (público) |
| Privacy token | `cli.Store.PrivacyTokens.GetPrivacyToken` (público) |

A call-key (32 bytes) é embalada em `waE2E.Message{Call:&waE2E.Call{CallKey:…}}`
(proto gerado do whatsmeow — campo existe no commit pinado) e cifrada por device.

> **Achado-chave:** nenhuma API **privada** do whatsmeow é acessada. Tudo é público
> (Client) ou pelo facade **`DangerousInternals()`** (que existe justamente para
> expor essas operações de baixo nível). **Não é estritamente necessário alterar o
> core do whatsmeow para compilar** — o "dangerous" é aviso de **instabilidade**
> entre versões, não barreira de acesso.

## 4. Fluxo de sinalização + mídia (resumo)

- **Originar:** `CallManager.StartCall` → resolve LID, gera call-key + SSRCs (HKDF) →
  `BuildOfferStanza` (USync devices → `EncryptMessageForDevices` por device →
  `<call><offer>` com `<audio>`, `<net>`, `<capability>`, `<destination>`,
  `<encopt>`, privacy token, device-identity) → `Query` (espera ack).
- **Ack** → `ParseRelayFromAck` extrai endpoints de relay (`<te2>`), `self/peer_pid`,
  **chave hop-by-hop (30 bytes)**, tokens → conecta nos relays.
- **Receber:** `events.CallOffer` → decifra call-key, parseia relays, manda
  `<preaccept>`, dispara `OnIncoming` → app chama `AcceptCall` (`BuildAcceptStanza`).
- **Mídia:** **não há ICE/SDP peer-a-peer nem DTLS-SRTP**; conecta-se aos
  **servidores de relay** da WhatsApp via DataChannel pion (SDP reescrito, ICE
  candidate do relay, fingerprint DTLS fixo `WADTLSFingerprint`), e por cima manda
  **STUN feito à mão** (atributos WhatsApp: `SENDER-SUBSCRIPTIONS`, `SSRC-LIST`,
  `MESSAGE-INTEGRITY`, `FINGERPRINT`) com subscrições SSRC em protobuf.
- **SRTP/codec:** keys por direção/por-device via HKDF da call-key; **SRTP à mão**
  (AES-128-CTR + HMAC-SHA1, tag 4 bytes). Uplink PCM 16 kHz → **MLow.Encode**
  (PT=120) → RTP → `Protect` → relays; downlink relays → `Unprotect` → **MLow.Decode**
  → PCM. Codec é **sempre MLow** (offer anuncia Opus, mas o fio é MLow/PT 120).

## 5. Riscos

1. **Pin de whatsmeow muito específico.** O código depende das assinaturas exatas
   de `EncryptMessageForDevices`, `DecryptDM`, `WaitResponse/CancelResponse` e do
   `waE2E.Call.CallKey` no commit `5f04eac…` (2026-06-22). Se o nosso fork divergir,
   **reconciliar essas assinaturas é a principal fricção** (não acesso — tudo é
   exportado). Mitigação: alinhar o adapter a um ponto de sync conhecido e cobrir
   com testes de compilação.
2. **Dependência forte de `DangerousInternals()`** — instável por design entre
   versões; qualquer refactor upstream de send-node/Signal quebra o adapter.
   Mitigação: concentrar o shim de compatibilidade em **um arquivo** (já é assim).
3. **Crypto e constantes feitas à mão / reverso-engenheiradas** (SRTP, STUN,
   `WADTLSFingerprint`, `WARelayPort=3480`, PT=120, blobs de capability). A WhatsApp
   pode mudar isso **server-side** e quebrar chamadas sem bump de versão. Tratar
   como **frágil**; precisa de owner de manutenção.
4. **`AssertSessions` é no-op** — depende de `EncryptMessageForDevices` estabelecer
   sessões Signal preguiçosamente; pode falhar para devices/peers "frios".
5. **Peso no `go.mod`** (árvore pion). Mitigação: módulo/submódulo separado.
6. **Manutenção e proveniência:** WaCalls é um fork com workflow de sync-upstream —
   o projeto canônico está em outro lugar; validar origem antes de depender a longo
   prazo. Crédito a `whatsapp-rust`/`zapo`/outros no README.
7. **⚠️ Banimento (o maior risco de negócio).** VoIP não-oficial é altamente
   sensível. **Somente conta descartável**; jamais atendzappy. Avaliar a API oficial
   (WhatsApp Business Calling API) antes de qualquer uso real.

## 6. Proposta de fases

> Cada fase = branch + PR próprios, **sem merge** sem aprovação, testes em conta
> descartável, sem deploy.

- **Fase 0 — Spike de compatibilidade (1–2 dias).** Sem feature: criar um pacote
  `voip` no fork, copiar `internal/voip/core` + o adapter `VoipSocket` e fazer
  **compilar** contra o nosso `*whatsmeow.Client` (resolve o risco #1/#2 cedo).
  Entregável: build verde + lista de divergências de assinatura, se houver.
- **Fase 1 — Receber/encerrar (sinalização).** Portar `signaling` + `call`
  (FSM) + adapter; **só** receber `events.CallOffer` e responder `<preaccept>`/
  `<reject>`/`<terminate>` (sem mídia). Teste: originar do app oficial → o fork
  detecta a chamada e rejeita/encerra de forma limpa. Sem áudio ainda.
- **Fase 2 — Mídia (MLow + SRTP + transporte).** Portar `media`, `media/mlow`,
  `transport`. Conectar relays, negociar SSRC, áudio **um sentido** (downlink:
  ouvir o par). Teste: aceitar uma chamada e receber áudio.
- **Fase 3 — Áudio bidirecional + originar.** `StartCall` completo + uplink
  (capturar PCM de uma fonte de teste) + `mute`. Teste: chamada 1:1 completa
  fork↔app oficial em conta descartável.
- **Fase 4 — Robustez.** Reconexão de relay, múltiplas chamadas, métricas/timeouts,
  testes de vetor do MLow no nosso CI, documentação.

**Recomendação de empacotamento:** módulo/submódulo `whatsmeow-voip` separado
(isola pion do `go.mod` principal; mantém o core de mensagens enxuto).

## 7. Decisões que preciso de você (Tech Lead) antes de codar

1. **Empacotamento:** submódulo/módulo separado `whatsmeow-voip` (recomendado) vs
   pacote `voip` no módulo principal (incha o `go.mod` com pion)?
2. **Pin/sync:** alinhar o fork a um commit cuja superfície `DangerousInternals`/
   `waE2E.Call` case com o WaCalls, ou adaptar o adapter ao nosso HEAD?
3. **Escopo inicial:** parar na Fase 1 (sinalização/rejeição — baixo risco) para
   validar viabilidade antes de investir em mídia?
4. **Risco/negócio:** seguir com VoIP não-oficial em conta descartável para P&D, ou
   priorizar avaliação da **WhatsApp Business Calling API** oficial?

Aprovado o plano (e as decisões acima), inicio pela **Fase 0** em branch separada.
