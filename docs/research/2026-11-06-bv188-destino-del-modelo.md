# BV-188 · lectura: adónde envía el código cada agente (destino del modelo)

Etapa 2 de la investigación «varios dispositivos» (el documento de diseño vive en
`qtorb/rosmarops-vigil`). **Solo lectura**: no se implementa nada; el advisor cierra la
parte de proveedores y países con GTM-92.

Pregunta: dónde guarda cada agente leído hoy el servidor de modelo
(`ANTHROPIC_BASE_URL` y equivalentes de Codex, Cline, Continue, Hermes, Cursor,
OpenCode, Gemini CLI), y qué hosts aparecen.

## Qué lee hoy RosmarOps

Hoy RosmarOps solo deduce un **destinatario por categoría** (nunca una URL ni un host
literal), y solo para Claude Code; del resto no lee el endpoint del modelo.

- **Claude Code** — se lee. El servidor del modelo lo fijan variables de entorno en las
  capas de `ClaudeSettings` (managed, local, project, user y `claudeCode.environmentVariables`
  de VS Code).
  - `internal/rules/overview.go:874-880` — `providerVars`: `CLAUDE_CODE_USE_BEDROCK` →
    `aws_bedrock`; `CLAUDE_CODE_USE_VERTEX` → `google_vertex`; `CLAUDE_CODE_USE_FOUNDRY` →
    `microsoft_foundry`; `CLAUDE_CODE_USE_ANTHROPIC_AWS` → `anthropic_aws`;
    `ANTHROPIC_BASE_URL` → `custom_gateway`.
  - `internal/rules/overview.go:886-919` — `claudeRecipient`: recorre las capas por
    precedencia (managed → local → project → user → VS Code) y devuelve la categoría. El
    **valor se compara, nunca se guarda** (`overview.go:912` lo revela y compara; el
    comentario de `overview.go:885` dice «Values are compared, never kept»). En el informe
    solo queda la categoría (`cell.Data.Recipient`, `overview.go:861`), no la URL ni el host.
  - Sin variable: `internal/rules/overview.go:862-864` → `anthropic_api` (default del
    producto, asumido).
- **Cursor** — `internal/rules/overview.go:856-858` → `vendor_backend` (default del producto,
  asumido; `ByoKeyStillRouted`). No se lee su endpoint de su configuración.
- **El resto (Codex, Cline, Continue, Hermes, OpenCode, Gemini CLI)** —
  `internal/rules/overview.go:866-867` (rama `default`) → `not_readable`. No se lee ningún
  servidor de modelo de su configuración.
  - **Codex**: su `.credentials.json` se lee solo para los tokens;
    `internal/scan/credfiles/agentauth.go:22-23` dice explícitamente que `server_name`,
    `server_url`, los client-id, la caducidad y el proveedor activo **no se leen**.
  - **Hermes**: se lee `terminal.backend` (`internal/rules/overview.go:324`
    `hermesUnknownBackends`, BV-111), pero eso es el backend de terminal/shell para calcular
    el alcance, **no** el servidor del modelo.
  - **Cline, Continue, OpenCode, Gemini CLI**: no se lee (ni se busca) su endpoint de modelo
    en ningún colector (`internal/scan/...`) ni en la base de conocimiento
    (`internal/knowledge`).

## Qué hosts aparecen

Ninguno literal. En el código solo están las **categorías** de destinatario
(`internal/rules/overview.go:96-105`): `anthropic_api`, `custom_gateway`, `aws_bedrock`,
`google_vertex`, `microsoft_foundry`, `anthropic_aws`, `vendor_backend`, `not_readable`.
No hay nombres de host de servidores de modelo ni en el código ni en `internal/knowledge`.
(El único host que el programa conoce es el del espacio, `app.rosmarops.com`, en
`internal/spacenet`, que no tiene relación con el modelo de ningún agente.)

## Resumen

- El único endpoint de modelo que se lee hoy es el de Claude Code, y solo como categoría
  (nunca la URL/host literal; el valor se compara y se descarta).
- Cursor → `vendor_backend` (asumido). El resto → `not_readable`.
- No hay hosts de modelo literales en el repositorio.
