---
PLAN: "feat: unify codejob state in PLAN.md frontmatter, agent roles, and the cloud loop"
TAG: v0.5.0
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 12119983344966392524
---

# Plan — Estado único en `docs/PLAN.md`, roles de agente y loop en la nube

Plan de ejecución para un agente. El **comportamiento objetivo** ya está descrito
en la documentación (que este plan implementa):

- Comportamiento y uso: [`docs/CODEJOB.md`](CODEJOB.md)
- Diagramas y mapa de pruebas: [`docs/diagrams/CODEJOB_FLOW.md`](diagrams/CODEJOB_FLOW.md)

Implementa el código hasta que coincida con esos documentos y todas las pruebas
de §5 pasen.

## 1. Objetivo

Hoy el estado de `codejob` se reparte entre `.env` (sesión efímera, local,
gitignored) y `docs/PLAN.md`. Por eso el loop **solo vive en la PC**: un runner de
la nube arranca sin ese `.env`. Movemos **todo** el estado al frontmatter de
`docs/PLAN.md`; como se commitea, cada transición queda en git y el loop entero
(despachar → revisar → publicar) corre en GitHub Actions. Además añadimos un
**agente revisor** opcional como compuerta de calidad antes del humano.

Resultado: editar la cabecera y commitear despacha; fusionar el PR publica. Sin
abrir la PC.

## 2. Principios de ejecución

- **TDD estricto.** Para cada comportamiento: primero el test (rojo), luego el
  código (verde). El mapa de §5 es la lista mínima; ninguna arista del flujo queda
  sin test.
- **Todo con mocks, sin red real.** Ninguna prueba toca red, `git`, `gh` ni el
  keyring reales. Se inyectan dobles (ver §4.1, seam de `Runner`).
- **Break change limpio.** Se **elimina** el código viejo (`.env`/`CODEJOB`,
  `CHECK_PLAN.md`, claves de keyring antiguas). Sin alias, sin migración
  automática, sin ramas de compatibilidad.
- **No romper `gopush`.** El cierre sigue siendo `gopush` tag-only (no `gorelease`).

## 3. Decisiones tomadas (defaults fijados, ya no son preguntas)

| # | Decisión |
|---|---|
| Estado | Único en el frontmatter de `docs/PLAN.md`; `.env`/`CHECK_PLAN.md` eliminados. |
| Tokens | Nombre único keyring = env = secret: `JULES_API_KEY`, `GH_TOKEN`. Sin alias. |
| Runner CI | `ubuntu-latest`; bootstrap `go install …/cmd/codejob@<versión-fijada>`. |
| Publicación CI | `gopush` tag-only, `--no-cascade`, sin backup. |
| Revisor | Juzga (postea review nativa de GitHub). No commitea. |
| Corrección | La orquesta codejob ante `CHANGES_REQUESTED`; corrector = `EXECUTOR` salvo `CORRECTOR`. |
| Tope de rondas | `ROUND` máximo **3**; superado → pasa a revisión humana. |
| Cierre | El **humano fusiona** (sin auto-merge); la fusión publica. |
| Revisores | Uno (`REVIEWER`); lista en cadena queda para después. |
| Secrets | Por org con `--init-action --org` donde exista org; por-repo en cuentas personales. |
| Correlación | Por **rama/identidad**, no por session id (los eventos de GitHub no traen el session id). |

## 4. Cambios por archivo

### 4.1 Testabilidad — seam de `Runner` (habilita todo el TDD)
- **Nuevo** `Runner` interface (generaliza `SecretRunner` de `github_secrets.go`)
  con `Run(name string, args ...string) (string, error)`. Inyectable en las
  funciones de estado. Un `defaultRunner` envuelve `webtyp/command`.
- Reescribir `CheckoutPRBranch`, `MergePR`, `MergeAndPublish`, `resolveDefaultBranch`
  para usar el `Runner` inyectado (hoy llaman `command.Run` directo → no mockeable).

### 4.2 Estado en el frontmatter
- `frontmatter.go` — `PlanMeta` gana `Executor, Reviewer, Corrector, ReviewGuide,
  Status, Session, ReviewSession, Round, PR` + constantes de clave. **Escritor**
  que actualiza solo el bloque de frontmatter preservando el cuerpo.
- `codejob.go` / `codejob_state.go` — **eliminar** `.env`/`CODEJOB` (parseo,
  legacy, `CODEJOB_PR`, `LoadCodejobState`/`SaveCodejobState`) y el renombrado a
  `CHECK_PLAN.md` (`HandleDone`, `.gitignore CHECK_*.md`). El estado se lee/escribe
  en el frontmatter. Sin código de migración.

### 4.3 Roles y drivers
- `interface.go` — `CodeJobDriver.Send(prompt, title)` → `Send(JobSpec{Role,
  Branch, PlanPath, Prompt, Title})`; registro de drivers por rol.
- `code_jules.go` — adaptar `JulesDriver` a `JobSpec`; soportar rol `executor`
  (implementar el plan) y `reviewer` (revisar el PR de la rama `X` y postear una
  review nativa).
- **Nuevo**: despacho del revisor y lectura del veredicto vía `Runner`
  (`gh pr view --json reviews`), con el tope `ROUND` (=3).

### 4.4 Auth / tokens
- `codejob_auth.go` / `codejob_gh_auth.go` — renombrar claves de keyring a
  `JULES_API_KEY` y `GH_TOKEN` (borrar `jules_api_key`, `github_pat`,
  `github_token`). Leer primero de la **variable de entorno del mismo nombre**
  (CI), cayendo al keyring (local).

### 4.5 CLI y Action
- `cli.go` — parsear `--ci <phase>` (`dispatch|review|verdict|publish`),
  `--init-action`, `--force`, `--org`, `--visibility`.
- `cmd/codejob/main.go` — atender los flags nuevos; actualizar `showHelp()`.
- **Nuevo** `codejob_action.go` + `templates/codejob.yml` embebido (`go:embed`):
  `InitCodejobAction(force bool, org, visibility string)` crea
  `.github/workflows/codejob.yml` (idempotente) y registra `JULES_API_KEY`+`GH_TOKEN`.
- `github_secrets.go` — `SetSecret` gana soporte `--org`/`--visibility`.

### 4.6 Documentación (ya actualizada a objetivo; mantener en sync)
- `docs/CODEJOB.md`, `docs/diagrams/CODEJOB_FLOW.md`,
  `docs/codejob/JULES_AUTOMATION.md` describen ya el estado final.

## 5. Mapa de pruebas (TDD)

Refleja la traza de [`CODEJOB_FLOW.md`](diagrams/CODEJOB_FLOW.md#traceability-test-map).
Todas con dobles: `Runner` falso (git/gh), driver falso (executor/reviewer),
`Publisher` mock, `SecretRunner` mock, keyring/env falsos.

| Comportamiento | Test | Mock |
|---|---|---|
| Leer estado del frontmatter | `TestPlanState_ReadFrontmatter` | temp file |
| Escribir estado preservando el cuerpo | `TestPlanState_WritePreservesBody` | temp file |
| `STATUS` derivado cuando falta | `TestPlanState_StatusDerivation` | temp file |
| Token: env → keyring, mismo nombre | `TestAuth_EnvVarThenKeyring` | keyring/env falsos |
| Parseo `--ci <phase>` / flags init | `TestParseArgs_CIPhases`, `TestParseArgs_InitFlags` | — |
| dispatch → running | `TestCI_Dispatch_WritesRunning` | driver + Runner |
| running → reviewing (REVIEWER set) | `TestCI_PROpened_DispatchesReviewer` | driver + Runner |
| running → review (REVIEWER none) | `TestCI_PROpened_NoReviewer` | Runner |
| reviewing → review (APPROVED) | `TestCI_Verdict_Approved` | Runner (reviews json) |
| reviewing → running (CHANGES_REQUESTED, ROUND++) | `TestCI_Verdict_ChangesRequested_RoundInc` | driver + Runner |
| reviewing → review (ROUND > 3) | `TestCI_Verdict_RoundCap` | Runner |
| review → publicado (tag-only, borra plan) | `TestCI_Publish_TagOnly` | mock Publisher |
| publish no-op si falta el plan | `TestCI_Publish_NoopWhenNoPlan` | mock Publisher |
| `--init-action` crea/idempotente/`--force` | `TestInitAction_CreatesWhenAbsent`, `_Idempotent`, `_ForceOverwrites` | temp dir |
| Registro de secret repo y `--org` | `TestInitAction_SecretScope` | mock SecretRunner |
| Contrato del workflow embebido | `TestActionTemplate_Contract` | string embebido |

## 6. Criterios de aceptación (Definition of Done)

1. `gotest` verde (vet, tests, race) en todo el repo.
2. Todas las pruebas de §5 existen y pasan; se escribieron antes que su código.
3. **Cero** referencias en código a: `CODEJOB` en `.env`, `CODEJOB_PR`,
   `CHECK_PLAN`, `jules_api_key`, `github_pat`, `github_token`.
4. `codejob --init-action` genera un `.github/workflows/codejob.yml` que satisface
   `TestActionTemplate_Contract` (guard `merged==true` + gates por `STATUS`).
5. El comportamiento coincide con `docs/CODEJOB.md` y `docs/diagrams/CODEJOB_FLOW.md`.

## 7. Fuera de alcance

- `gorelease` en CI (binarios cross-platform): el cierre es tag-only.
- Re-dispatch nativo por comentario (Jules reaccionando solo): la corrección la
  orquesta codejob.
- Cadena de varios revisores (v1 = un `REVIEWER`).
- Cascade a módulos dependientes en CI (queda al flujo local).
