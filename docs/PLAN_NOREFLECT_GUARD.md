---
PLAN: "feat(gotest): noreflect check — wasm code may not compare interfaces or use reflection"
EXECUTOR: jules
REVIEWER: none
---

# Plan — Guardia `noreflect` en `gotest`

> Master: `webtyp/docs/NO_REFLECTION_MASTER_PLAN.md` (ola 4). **No despachar** hasta que las olas
> 1–3 estén publicadas: activo antes, rompería `gotest` (y las cascadas de `gopush`) en todo repo aún
> sin migrar. El orquestador lo despacha; el ejecutor no necesita verificar eso.

## 1. El problema

Regla del dueño: el código que compila a wasm no usa reflexión. En TinyGo, `==`/`!=`/`switch` entre
valores de interfaz compila a `runtime.interfaceEqual` → `reflectValueEqual(reflectlite.ValueOf…)`,
y con un solo sitio alcanzable `internal/reflectlite` entra entero en el binario (~7–9 KB medidos).
El caso más común es `err == ErrX`. Nada lo detecta hoy: se arregló una vez (olas 1–3) y el próximo
`err == X` lo vuelve a meter sin que nadie lo note.

## 2. Design gate (api-design)

1. **Antecedentes.** `go vet` (analizadores con `go/types` integrados al ciclo de test);
   `staticcheck`/`golangci-lint` (reglas configurables, por ejemplo `errorlint`, que hace lo contrario:
   empuja a `errors.Is`); la doc de TinyGo sobre limitaciones de reflexión (solo advierte, no
   detecta). Aquí la regla va dentro de `gotest`, el comando que define "listo" en todo el ecosistema,
   sin configuración: si el paquete compila a wasm, la regla aplica.
2. **Nombres.** El paso se llama `noreflect` en la salida de `gotest` (junto a `vet`, `race`, `wasm`).
   Función de librería: `ScanWasmReflection(dir string) ([]ReflectionFinding, error)`. Tipo
   `ReflectionFinding{File string; Line int; Kind ReflectionKind; Snippet string}` con
   `ReflectionKind` tipado (constantes `InterfaceComparison`, `InterfaceSwitch`, `ReflectImport`,
   `ReflectiveCall`).
3. **Balance.** Conceptos +1 (un paso más en la salida) · archivos a tocar para cumplir: 0 si el
   código ya cumple · formas de saltarlo: 0 (sin flag de exclusión: cerrado por defecto).
4. **Dónde va.** `devflow`, dueño de `gotest`. AGENTS.md: toda condición es una función exportada de
   la librería; `cmd/gotest` no cambia.
5. **Qué borra.** Nada: es capacidad nueva.

## 3. La corrección

1. Nuevo archivo `noreflect.go` en la raíz de la librería con `ScanWasmReflection(dir)`:
   - Carga con `golang.org/x/tools/go/packages` (agregar la dependencia) los paquetes `./...` de `dir`
     con `Env: GOOS=js GOARCH=wasm`, modo `NeedName|NeedFiles|NeedSyntax|NeedTypes|NeedTypesInfo`,
     **sin tests** (`Tests: false`): lo que no viaja en el binario no cuenta.
   - Recorre el AST de cada archivo del módulo (no dependencias) y reporta:
     - `InterfaceComparison`: `*ast.BinaryExpr` con `==` o `!=` donde algún operando tiene tipo cuyo
       `Underlying()` es `*types.Interface` y **ninguno** es `nil` sin tipo
       (`TypesInfo.Types[e].IsNil()`). `x == nil` y `x != nil` están permitidos.
     - `InterfaceSwitch`: `*ast.SwitchStmt` con `Tag` de tipo interfaz (los `switch x.(type)` son
       `*ast.TypeSwitchStmt` y están permitidos).
     - `ReflectImport`: import de `"reflect"`.
     - `ReflectiveCall`: llamadas a `errors.Is`, `errors.As`, `sort.Slice`, `sort.SliceStable`
       (resueltas por `TypesInfo.Uses`, no por texto).
   - Devuelve los hallazgos ordenados por archivo y línea. Rutas relativas a `dir`.
2. En `gotest.go`, `runFullTestSuite`: por cada directorio de `wasmDirs` (los mismos donde corre el
   paso wasm), llamar `ScanWasmReflection`. Si hay hallazgos: `addMsg(false, "noreflect")`,
   `testStatus = "Failed"`, e imprimir uno por línea:
   `noreflect: <archivo>:<línea>: <tipo>: <snippet> — use IsX(err) / compare a concrete field`.
   Sin hallazgos: `addMsg(true, "noreflect")`. Un error de carga se reporta como fallo, nunca se
   ignora.
3. Mensajes y la cadena `"noreflect"` como constantes con nombre (AGENTS.md).
4. `README.md`: una fila nueva en la tabla de pasos de `gotest` explicando la regla y cómo corregir
   cada tipo de hallazgo (`storage.IsNoRows`, `orm.IsNotFound`, comparar un campo concreto).

## 4. Tests (rojo primero, en `tests/`, sobre árboles en `t.TempDir()`)

Un módulo de prueba con `go.mod` y un paquete sin build tags:
- `err == errSentinel` (ambos `error`) → 1 hallazgo `InterfaceComparison` con la línea correcta.
- `err != nil`, `x == nil` → 0 hallazgos.
- `switch err { case errA: }` → `InterfaceSwitch`; `switch v := x.(type)` → 0.
- `import "reflect"` → `ReflectImport`; `errors.Is(err, e)` y `sort.Slice(...)` → `ReflectiveCall`.
- Un archivo con `//go:build !wasm` que contiene `err == e` → 0 (no compila a wasm).
- Un `_test.go` con `err == e` → 0.
- `==` entre dos punteros concretos o dos strings → 0.

## 5. Criterios de aceptación

- `gotest` en este repo verde (este repo no compila a wasm: el paso no aplica aquí).
- (Lo verifica el orquestador al revisar el PR, no el ejecutor: el `gotest` nuevo da `noreflect ✅`
  en `dom` y `form`, ya migrados.)
- Exportados nuevos: solo `ScanWasmReflection`, `ReflectionFinding`, `ReflectionKind` y sus
  constantes.

## 6. Restricciones

Las de `AGENTS.md`: biblioteca estándar permitida aquí (no compila a wasm), constantes con nombre,
tests en `tests/` sin tocar el home real ni la red, reutilizar `modfind` para recorrer módulos si hace
falta.
