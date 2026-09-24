---
PLAN: "fix(gopush): los dependientes se buscan desde la raíz del workspace, no solo en la carpeta padre"
EXECUTOR: jules
REVIEWER: none
STATUS: running
SESSION: 9312558786189124501
---

> Este plan se despacha con el flujo CodeJob. Ver skill: agents-workflow.
> Orquestador: `~/Dev/Project/docs/MODULE_VIEWS_MASTER_PLAN.md` (fase A1).

# Plan — `gopush` alcanza a todos los dependientes del workspace

## 1. El defecto

`gopush` actualiza en cascada los módulos que dependen del que se publica, pero solo
los busca en **la carpeta padre**:

- `cmd/gopush/main.go:83` → `goHandler.Push(..., "..")`
- `go_handler.go:396` (`Publish`) → `g.Push(..., "..")`
- `go_handler.go:233` → si `searchPath == ""`, usa `".."`.

Con este árbol:

```
~/Dev/Project/                 ← Project.code-workspace
├── webtyp/layout/             ← se publica aquí
├── webtyp/…                   ← ".." ve solo esto
└── veltylabs/
    ├── velty.code-workspace
    ├── mjosefa-cms/           ← nunca se actualiza
    └── modules/item_catalog/  ← ".." = veltylabs/modules/, tampoco ve mjosefa-cms
```

Publicar `webtyp/layout` nunca actualiza `veltylabs/mjosefa-cms`, y publicar
`veltylabs/modules/item_catalog` tampoco. La app de producción queda con versiones
viejas sin que nadie lo note.

## 2. Diseño

### Regla

La ruta de búsqueda por defecto pasa a ser la **raíz del workspace**: el ancestro **más
externo** del repo que contiene al menos un archivo `*.code-workspace`, subiendo como
máximo hasta el directorio **hijo** de `$HOME` (nunca `$HOME` ni más arriba). Si ningún
ancestro tiene uno, se usa `..` (el comportamiento de hoy).

Es "más externo" y no "más cercano" porque hay workspaces anidados:
`veltylabs/velty.code-workspace` está dentro de `Project/Project.code-workspace`. El más
cercano, desde `webtyp/layout`, dejaría fuera a `veltylabs/`.

### Design gate

1. **Prior art.** Cargo sube buscando el `Cargo.toml` con `[workspace]`; `go work`
   sube buscando `go.work`; Nx/Lerna suben buscando `nx.json`/`lerna.json`. Los tres
   derivan la raíz de un archivo marcador, no de una ruta fija. Aquí el marcador que ya
   existe es el `*.code-workspace` de VS Code. Se toma el más externo (Cargo también
   rechaza los workspaces anidados) porque en este árbol el anidado es real.
2. **Prueba del nombre.** `devflow.WorkspaceRoot(dir)` → "la raíz del workspace de
   este directorio". Constante `WorkspaceFileExt = ".code-workspace"`.
3. **Contabilidad de complejidad.** Conceptos +1 (el marcador de workspace). Archivos
   que tocar para que un dependiente nuevo se actualice: −1 (hoy hay que moverlo al
   lado del publicado). Formas de hacer lo mismo: 0 (`".."` deja de ser el default;
   sigue siendo el fallback de la misma regla, no un segundo camino).
4. **Dónde vive.** `devflow`, que es dueño de la cascada. `cmd/gopush` solo pasa `""`.
5. **Qué borra.** Los dos literales `".."` de `cmd/gopush/main.go:83` y
   `go_handler.go:396`.

## 3. Reglas de código (obligatorias)

- Este repo es **tooling de backend** y usa stdlib legítimamente (`os`,
  `path/filepath`, `strings`). **No** "arreglar" esos imports hacia `webtyp/fmt`.
- Ningún string repetido como literal: `WorkspaceFileExt` es constante exportada.
- `cmd/gopush/main.go` no gana lógica: solo cambia el argumento a `""`.
- Tests con `gotest` (nunca `go test`).

## 4. Etapas

### Etapa 1 — `workspace.go` (nuevo)

Crear `workspace.go` en la raíz del paquete `devflow`:

```go
// WorkspaceFileExt is the marker that makes a directory a workspace root.
const WorkspaceFileExt = ".code-workspace"

// WorkspaceRoot returns the OUTERMOST ancestor of dir (dir included) that
// contains a *.code-workspace file, never climbing to $HOME or above.
// It returns "" when no ancestor qualifies.
func WorkspaceRoot(dir string) string {
    home, _ := os.UserHomeDir() // "" on error: then only the disk root stops the climb
    return WorkspaceRootFrom(dir, home)
}

// WorkspaceRootFrom is WorkspaceRoot with an explicit home directory — the
// seam tests use to build a fake tree under t.TempDir().
func WorkspaceRootFrom(dir, home string) string
```

Algoritmo exacto de `WorkspaceRootFrom`:

1. `abs, _ := filepath.Abs(dir)`; si `home != ""`, `home, _ = filepath.Abs(home)`.
2. `found := ""`. Recorrer `d := abs` hacia arriba con `filepath.Dir`:
   - detenerse si `d == home`, o si `d == filepath.Dir(d)` (raíz del disco);
   - si `filepath.Glob(filepath.Join(d, "*"+WorkspaceFileExt))` devuelve ≥1 entrada
     que sea **archivo** (no directorio), `found = d` (y seguir subiendo).
3. Devolver `found`.

### Etapa 2 — el default de `Push`

En `go_handler.go:233`, reemplazar:

```go
if searchPath == "" {
    searchPath = ".."
}
```

por:

```go
if searchPath == "" {
    searchPath = WorkspaceRoot(g.rootDir)
    if searchPath == "" {
        searchPath = ".."
    }
}
```

`g.rootDir` puede ser relativo: `WorkspaceRoot` ya lo resuelve con `filepath.Abs`.

Actualizar el comentario de doc de `Push` (línea 218): `searchPath: Path to search for
dependent modules (default: the workspace root — see WorkspaceRoot — or ".." when
there is none)`.

### Etapa 3 — quitar los literales `".."`

- `cmd/gopush/main.go:83`: último argumento `".."` → `""`.
- `go_handler.go:396` (`Publish`): último argumento `".."` → `""`.
- **No** tocar `go_handler.go:552` (`depHandler.Push(..., "")` con
  `skipDependents=true`): ya pasa `""` y no cascadea.
- **No** tocar los tests existentes que pasan `".."` explícito: prueban el camino
  explícito, que sigue igual.

### Etapa 4 — el recorrido salta directorios que nunca tienen dependientes

Con la raíz del workspace, `FindDependentModules` (`go_mod.go:637`) y `findAllModules`
(`cascade.go:287`) recorren mucho más árbol. En **ambos** `filepath.Walk`, al entrar a
un directorio (`info.IsDir()`), devolver `filepath.SkipDir` si su nombre es
`node_modules` o `vendor`, o si empieza con `"."` (`.git`, `.vscode`, …). **Excepción:**
nunca saltar el propio `searchPath` aunque empiece con `.` (el fallback `..` empieza
con punto). Poner los nombres en una sola variable del paquete:

```go
// walkSkipDirs are directory names that never hold a dependent module.
var walkSkipDirs = []string{"node_modules", "vendor"}
```

y una función `skipWalkDir(path, root string, info os.FileInfo) bool` que usan los dos
recorridos.

### Etapa 5 — tests (`test/workspace_test.go`, nuevo)

Árbol temporal con `t.TempDir()` como `home` falso:

```
<home>/Dev/Project/Project.code-workspace
<home>/Dev/Project/org_a/lib/go.mod               (module example.com/lib)
<home>/Dev/Project/org_b/velty.code-workspace
<home>/Dev/Project/org_b/app/go.mod               (require example.com/lib v0.0.1)
<home>/Dev/Project/org_b/app/node_modules/x/go.mod (require example.com/lib v0.0.1)
```

Casos:

1. `WorkspaceRootFrom(".../org_a/lib", home)` → `<home>/Dev/Project` (el más externo,
   no `org_b`).
2. `WorkspaceRootFrom(".../org_b/app", home)` → `<home>/Dev/Project`.
3. Sin ningún `.code-workspace` en el árbol → `""`.
4. Un `x.code-workspace` **directamente en `home`** no cuenta → `""` si es el único.
5. Un **directorio** llamado `foo.code-workspace` no cuenta.
6. `FindDependentModules("example.com/lib", <home>/Dev/Project)` devuelve
   `org_b/app` y **no** `org_b/app/node_modules/x`.
7. Igual para `BuildDependentGraph`.

### Etapa 6 — documentación

- `docs/GOPUSH.md`: reemplazar toda mención de "busca dependientes en `..`" por la
  regla de §2, con el árbol de ejemplo de §1.
- README: si describe la cascada, el mismo cambio.

## 5. Verificación

```bash
gotest                                               # verde
grep -n '"\.\."' cmd/gopush/main.go go_handler.go    # solo quedan la línea del fallback en Push
```

Prueba real (local, después del tag): en `~/Dev/Project/webtyp/layout`, un `gopush`
con cambio trivial debe listar `veltylabs/mjosefa-cms` entre los dependientes
actualizados.

## 6. Tabla de etapas

| # | Etapa | Archivos |
|---|---|---|
| 1 | `WorkspaceRoot`, `WorkspaceRootFrom`, `WorkspaceFileExt` | `workspace.go` |
| 2 | default de `Push` | `go_handler.go` |
| 3 | quitar literales `".."` | `cmd/gopush/main.go`, `go_handler.go` |
| 4 | saltar `node_modules`, `vendor`, `.*` | `go_mod.go`, `cascade.go`, `workspace.go` |
| 5 | tests | `test/workspace_test.go` |
| 6 | docs | `docs/GOPUSH.md`, `README.md` |
