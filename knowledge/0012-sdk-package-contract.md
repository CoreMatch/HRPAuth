> 来源：HA-Contract@bab2a05（2026-11-07），原路径 docs/dev/HRPAuth/sdk-package.md（契约仓知识库回迁）。

# SDK Package Contract (Static Compile-Time Aggregation)

This document defines the **SDK package** format exchanged between the HRPAuth
main service and the WebUI SDK handler (`HA-WebUI-SDKHandler`). It replaces the
old runtime JS SDK model (`sdk_url` + `window['<service-name>-sdk']`) with a
compile-time aggregation model: SDKs are shipped as self-contained source
packages, aggregated into a generated frontend, and compiled by the SDK handler.

## Design Positioning

- HRPAuth is the **single trusted broker** for SDK packages. Satellite
  services upload SDK packages to HRPAuth; the SDK handler pulls them from
  HRPAuth only. The handler never reaches satellite services directly.
- HRPAuth **validates** every uploaded package (archive safety, manifest
  correctness, conflict policy) before accepting it.
- The SDK handler aggregates all accepted packages into a fresh frontend build
  and publishes the compiled static assets to its configured artifacts
  directory.
- Satellite services are deployed by the same operator as the WebUI; all
  satellite services are absolutely trusted by HRPAuth, and all authentication
  is performed by HRPAuth (Level 2 / Ops).

## Data Flow

```
Satellite service                    HRPAuth (main)                   HA-WebUI-SDKHandler
--------------                       ----------------                 ---------------------
POST /services/sdk-packages  ──►     validate + store (fs + Redis)
                                     GET /services/sdk-packages   ◄──  poll (fingerprint)
                                     GET /services/sdk-packages/:name ◄─ download archive
                                                                        copy template
                                                                        extract + inject SDKs
                                                                        merge npm deps
                                                                        tsc + vite build
                                                                        publish to artifacts dir
```

## Package Format

A SDK package is a single compressed archive of exactly one of:

- `tar.gz` (gzip-compressed tar)
- `zip`

The archive MUST contain exactly one `manifest.json` at its root. All other
files are SDK source files (TS/TSX/JS/JSON/CSS/static assets).

### Layout

```text
example-service-sdk.tar.gz
  manifest.json
  src/
    index.ts
    pages/
      ExamplePage.tsx
    components/
      Widget.tsx
  styles/
    example.css
```

- Relative paths inside the archive are preserved on injection.
- Path traversal (`..`, absolute paths) is rejected by HRPAuth and again by the
  SDK handler.
- Symbolic links and hard links are rejected.
- Allowed source extensions: `.ts`, `.tsx`, `.js`, `.jsx`, `.mjs`, `.cjs`,
  `.json`, `.css`, plus static assets referenced by the source
  (`.png`, `.jpg`, `.jpeg`, `.gif`, `.svg`, `.webp`, `.woff`, `.woff2`, `.ttf`).

### Size Limits

- Maximum uncompressed and compressed package size is enforced by HRPAuth
  (`sdk_packages.max_package_size`, default `20971520` = 20 MiB).
- Exceeding the limit returns `413 sdk_package_too_large`.

## manifest.json Schema

```json
{
  "schema_version": 1,
  "name": "example-service",
  "version": "1.0.0",
  "routes": [
    {
      "path": "/example",
      "module": "src/pages/ExamplePage",
      "title": "Example"
    }
  ],
  "menu": [
    {
      "label": "Example",
      "path": "/example"
    }
  ],
  "dashboard": [
    {
      "label": "Example",
      "path": "/example"
    }
  ],
  "critical_operations": [
    {
      "label": "Review sensitive settings",
      "path": "/example"
    }
  ],
  "profile_cards": [
    {
      "title": "Example Profile Card",
      "module": "src/profile/ExampleProfileCard",
      "priority": 100
    }
  ],
  "dependencies": {
    "some-lib": "^1.0.0"
  },
  "run_scripts": false
}
```

### Field Reference

| Field | Type | Required | Description |
|---|---|---|---|
| `schema_version` | int | yes | Manifest schema version. Currently `1`. |
| `name` | string | yes | Unique service name. `[a-z0-9]([a-z0-9-]*[a-z0-9])?`, max 64 chars. Uniquely identifies the package on HRPAuth. |
| `version` | string | yes | Semantic version (`major.minor.patch`). |
| `routes` | array | yes | Non-empty list of route entries (see below). HRPAuth core upload validation rejects every package whose `routes` is missing or empty, regardless of any `profile_cards`. |
| `routes[].path` | string | yes | Route path, MUST start with `/`, MUST NOT end with `/` (except root `/`), MUST NOT contain `?`/`#`/`..` segments. Becomes a React Router path. |
| `routes[].module` | string | yes | Relative path to a TS/TSX module inside the package (extension optional). The module MUST have a **default export** React component (used with `React.lazy`). MUST be inside the package (no `..`, no absolute path). |
| `routes[].title` | string | no | Optional document title hint. |
| `menu` | object/array | no | When present, adds one or more navbar items. Can be a single object or an array of objects. |
| `menu[].label` | string | yes* | Navbar display label. |
| `menu[].path` | string | yes* | Target route path, MUST reference an entry in `routes`. |
| `dashboard` | object/array | no | When present, adds one or more sidebar items on the Dashboard page. Can be a single object or an array of objects. |
| `dashboard[].label` | string | yes* | Dashboard display label. |
| `dashboard[].path` | string | yes* | Target route path, MUST reference an entry in `routes`. |
| `critical_operations` | object/array | no | Adds one or more route links to the host Account Security page's Critical Operations section. Can be a single object or an array of objects. |
| `critical_operations[].label` | string | yes* | Display label for the operation link. MUST NOT be blank. |
| `critical_operations[].path` | string | yes* | Target route path, MUST reference an entry in this package's `routes`. |
| `profile_cards` | object/array | no | When present, adds one or more cards to the host Profile page. Can be a single object or an array of objects. |
| `profile_cards[].title` | string | yes* | Card title rendered by the host Profile page. |
| `profile_cards[].module` | string | yes* | Relative path to a TS/TSX module inside the package (extension optional). The module MUST have a **default export** React component, and is rendered as the card body inside the host-provided card shell. MUST be inside the package (no `..`, no absolute path). |
| `profile_cards[].priority` | int | no | Optional explicit display priority. Higher numbers render closer to the top. When omitted, the card falls back to SDK aggregation/load order. |
| `dependencies` | object | no | npm dependencies to merge into the generated frontend `package.json`. Keys are package names, values are npm version ranges. |
| `run_scripts` | bool | no | Accepted in the manifest and carried through the handler's option plumbing, but the current build pipeline does not read it: dependency installation happens once at template setup (see Build Policy), and per-package script control is not applied. The handler does enforce a configured npm blacklist. |

> `menu.path`, `dashboard.path`, and `critical_operations.path` reference
> **routes declared by the same service**. Cross-service references are not
> allowed.

> `profile_cards` entries do not need a matching route entry of their own, but
> the package must still declare a non-empty `routes` array (see above).

### Profile Card Priority Contract

- `profile_cards[].priority` is the **resolved final priority value** seen by
  the host after the SDK provider applies its own deployment configuration.
- Every SDK provider that declares `profile_cards` **MUST** expose a clear
  deployment-facing configuration item so the deployer can set the priority for
  each card.
- If the deployer does not set a priority, the SDK provider may:
  - provide a default priority value, or
  - omit `priority` entirely.
- Host rendering order is:
  1. cards with explicit `priority`, sorted by descending numeric value
  2. cards without `priority`, sorted by SDK aggregation/load order
  3. ties keep stable SDK aggregation/load order

### Conflict Policy

- A package is uniquely identified by `name`.
- Uploading a new package whose `name` already exists is **rejected**
  (`409 sdk_package_conflict`). Replace requires `DELETE` first.
- Dependency conflicts are **not** detected at upload time; they surface as a
  build failure in the SDK handler, which keeps the previous published version.

## Validation Rules (enforced by HRPAuth at upload)

1. Archive format must be `tar.gz` or `zip`.
2. Archive must contain exactly one root-level `manifest.json`.
3. `manifest.json` must parse and satisfy the schema above.
4. All file paths must be relative, inside the package, free of `..` and
   absolute segments; no symlinks/hard links.
5. At least one of `routes` / `profile_cards` must be non-empty.
6. `menu.path`, `dashboard.path`, and `critical_operations.path` must reference a route in the same package.
7. `routes[].module` and `profile_cards[].module` must resolve inside the package.
8. Package size must not exceed `max_package_size`.
9. Duplicate `name` → `409 sdk_package_conflict`.

## Generated Frontend Contract (HA-WebUI-SDKHandler)

The handler produces the following generated modules under
`src/generated/` in the build frontend:

| File | Exports | Consumer |
|---|---|---|
| `sdk-manifest.ts` | `sdkManifest: SdkManifestEntry[]` | diagnostics / metadata |
| `sdk-routes.tsx` | `sdkRouteElements: ReactNode[]` (`<Route>` elements for direct insertion into `<Routes>`) and `sdkRoutes: RouteObject[]` (lazy components) | `App.tsx` consumes `sdkRouteElements` in its route tree |
| `sdk-navbar.tsx` | `sdkNavbarItems: SdkNavbarItem[]` | `Navbar.tsx` |
| `sdk-dashboard.tsx` | `sdkDashboardItems: SdkDashboardItem[]` | `Dashboard.tsx` |
| `sdk-critical-operations.ts` | `sdkCriticalOperations: SdkCriticalOperationItem[]` | `AccountSecurity.tsx` |
| `sdk-profile-cards.tsx` | `sdkProfileCards: SdkProfileCardItem[]` | `Profile.tsx` |

During injection the handler also rewrites cross-package relative imports:
when an SDK source file imports a module that lies outside its own package
directory (e.g. a bridge module reaching into host-relative paths), the import
specifier is rewritten so the build resolves against the host tree.

Type contracts are defined in `HRPAuth-Web/src/types/generated-sdk.ts`
(human-maintained, stable):

```ts
export interface SdkManifestEntry {
  name: string;
  version: string;
  routes: { path: string; module: string; title?: string }[];
  menu?: { label: string; path: string }[];
  dashboard?: { label: string; path: string }[];
  profileCards?: { title: string; module: string; priority?: number }[];
  runScripts: boolean;
  sha256: string;
}

export interface SdkNavbarItem {
  key: string;      // service name
  label: string;
  path: string;
}

export interface SdkDashboardItem {
  key: string;      // service name
  label: string;
  element: React.ReactNode; // lazy component rendering route content
}

export interface SdkCriticalOperationItem {
  key: string;      // service name + operation index
  label: string;
  path: string;     // route path declared by the same SDK
}

export interface SdkProfileCardItem {
  key: string;      // service name or service name + index
  title: string;
  priority?: number;
  loadOrder: number; // stable aggregation order from SDK handler
  element: React.ReactNode; // lazy component rendering card body
}
```

Every SDK service directory is injected under
`src/generated/services/<name>/` preserving the package's internal layout.

## Build Policy

Current behavior of `HA-WebUI-SDKHandler`:

- The template directory (default `./HRPAuth-WebUI`, configurable via
  `build.template_dir` / `build.template_git_url`) is cloned/pulled by the
  handler and is read-write: the handler runs `git checkout .` + `git pull`
  on it and runs `npm install` there once during template setup.
- Every build copies the template (including its `node_modules`) into a fresh
  work directory; all SDK packages are re-injected on every build (no
  incremental leftovers).
- npm dependencies from every manifest are merged into the work directory's
  `package.json`; there is no per-build `npm install`, so dependencies that
  are not already present in the template's `node_modules` are not installed.
- Build pipeline: `npm run build` when the template defines a `build` script,
  otherwise `tsc -b` followed by `vite build`.
- Publishing writes to the configured fixed artifacts directory: the new
  `dist` is copied to a `.tmp` sibling, the previous directory is removed,
  and the `.tmp` directory is renamed into place. There is no versioned
  artifact history and no separate `current` pointer; between the removal and
  the rename the artifacts directory is absent.

## Error Codes

New error codes introduced by this contract (registered in
`docs/api/error-codes.md`):

| Code | Meaning |
|---|---|
| `sdk_package_conflict` | A package with the same name already exists |
| `sdk_package_not_found` | Requested SDK package does not exist |
| `sdk_package_invalid` | Archive is malformed or unreadable |
| `sdk_package_invalid_manifest` | `manifest.json` is missing or fails validation |
| `sdk_package_invalid_path` | Archive contains illegal path (traversal/absolute/symlink) |
| `sdk_package_too_large` | Package exceeds the configured size limit |
